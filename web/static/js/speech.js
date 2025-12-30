// Web Speech API wrapper for PIKA

class PikaSpeech {
    constructor() {
        this.recognition = null;
        this.synthesis = window.speechSynthesis;
        this.isListening = false;
        this.alwaysListen = false;
        this.wakeWord = 'pika';
        this.wakeWordDetected = false;
        this.listeners = new Map();
        this.currentTranscript = '';
        this.voices = [];
        this.selectedVoice = null;
        this.voiceRate = 1.0;
        this.voicePitch = 1.0;
        this.networkErrorCount = 0;
        this.maxNetworkRetries = 3;

        this.initRecognition();
        this.initVoices();
    }

    initVoices() {
        // Load voices (may be async)
        const loadVoices = () => {
            this.voices = this.synthesis.getVoices();

            // Try to restore saved voice preference
            const savedVoiceName = localStorage.getItem('pika_voice');
            if (savedVoiceName) {
                this.selectedVoice = this.voices.find(v => v.name === savedVoiceName);
            }

            // Default to a good English voice if none selected
            if (!this.selectedVoice) {
                this.selectedVoice = this.voices.find(v =>
                    v.lang.startsWith('en') && (v.name.includes('Samantha') || v.name.includes('Google') || v.name.includes('Daniel'))
                ) || this.voices.find(v => v.lang.startsWith('en'));
            }

            this.emit('voices_loaded', this.voices);
        };

        // Chrome loads voices async
        if (this.synthesis.onvoiceschanged !== undefined) {
            this.synthesis.onvoiceschanged = loadVoices;
        }
        loadVoices();
    }

    getVoices() {
        return this.voices;
    }

    setVoice(voiceName) {
        this.selectedVoice = this.voices.find(v => v.name === voiceName);
        if (this.selectedVoice) {
            localStorage.setItem('pika_voice', voiceName);
        }
    }

    setRate(rate) {
        this.voiceRate = Math.max(0.5, Math.min(2.0, rate));
        localStorage.setItem('pika_voice_rate', this.voiceRate.toString());
    }

    setPitch(pitch) {
        this.voicePitch = Math.max(0.5, Math.min(2.0, pitch));
        localStorage.setItem('pika_voice_pitch', this.voicePitch.toString());
    }

    initRecognition() {
        const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;

        if (!SpeechRecognition) {
            console.error('Speech recognition not supported');
            this.emit('unsupported');
            return;
        }

        this.recognition = new SpeechRecognition();
        this.recognition.continuous = true;
        this.recognition.interimResults = true;
        this.recognition.lang = 'en-US';

        this.recognition.onstart = () => {
            console.log('Speech recognition started');
            this.isListening = true;
            this.networkErrorCount = 0; // Reset on successful start
            this.emit('start');
        };

        this.recognition.onend = () => {
            console.log('Speech recognition ended');
            this.isListening = false;
            this.emit('end');

            // Restart if always listening
            if (this.alwaysListen && !this.networkErrorCount) {
                setTimeout(() => this.start(), 100);
            }
        };

        this.recognition.onerror = (event) => {
            console.error('Speech recognition error:', event.error);
            this.emit('error', event);

            // Handle specific errors
            switch (event.error) {
                case 'not-allowed':
                    this.emit('permission_denied');
                    break;
                case 'network':
                    // Chrome's Web Speech API requires internet (uses Google servers)
                    this.networkErrorCount++;
                    if (this.networkErrorCount < this.maxNetworkRetries) {
                        console.log(`Network error, retrying (${this.networkErrorCount}/${this.maxNetworkRetries})...`);
                        setTimeout(() => this.start(), 1000 * this.networkErrorCount); // Exponential backoff
                    } else {
                        this.emit('network_error');
                        this.networkErrorCount = 0; // Reset for next attempt
                    }
                    break;
                case 'no-speech':
                    // User didn't say anything - just restart if always listening
                    break;
                case 'aborted':
                    // Recognition was aborted - normal during stop()
                    break;
                default:
                    console.warn('Unhandled speech error:', event.error);
            }
        };

        this.recognition.onresult = (event) => {
            let interimTranscript = '';
            let finalTranscript = '';

            for (let i = event.resultIndex; i < event.results.length; i++) {
                const transcript = event.results[i][0].transcript;
                const confidence = event.results[i][0].confidence;

                if (event.results[i].isFinal) {
                    finalTranscript += transcript;
                    this.handleFinalTranscript(transcript.trim(), confidence);
                } else {
                    interimTranscript += transcript;
                    this.emit('interim', { text: transcript, confidence: confidence });
                }
            }

            this.currentTranscript = interimTranscript || finalTranscript;
            this.emit('transcript', { interim: interimTranscript, final: finalTranscript });
        };
    }

    handleFinalTranscript(text, confidence) {
        console.log('Final transcript:', text, 'confidence:', confidence);

        const lowerText = text.toLowerCase();

        // Check for wake word
        if (this.alwaysListen) {
            if (lowerText.includes(this.wakeWord)) {
                this.wakeWordDetected = true;

                // Extract command after wake word
                const wakeWordIndex = lowerText.indexOf(this.wakeWord);
                const command = text.substring(wakeWordIndex + this.wakeWord.length).trim();

                if (command) {
                    this.emit('command', {
                        text: command,
                        wakeWord: true,
                        confidence: confidence,
                        fullText: text
                    });
                } else {
                    // Wake word only - wait for command
                    this.emit('wake_word_detected');
                }
            } else if (this.wakeWordDetected) {
                // Command following wake word
                this.emit('command', {
                    text: text,
                    wakeWord: true,
                    confidence: confidence
                });
                this.wakeWordDetected = false;
            }
        } else {
            // Not in always-listen mode, treat as direct command
            this.emit('command', {
                text: text,
                wakeWord: false,
                confidence: confidence
            });
        }
    }

    start() {
        if (this.recognition && !this.isListening) {
            try {
                this.recognition.start();
            } catch (error) {
                console.error('Failed to start recognition:', error);
            }
        }
    }

    stop() {
        if (this.recognition && this.isListening) {
            this.recognition.stop();
        }
    }

    toggle() {
        if (this.isListening) {
            this.stop();
        } else {
            this.start();
        }
    }

    setAlwaysListen(enabled) {
        this.alwaysListen = enabled;
        if (enabled && !this.isListening) {
            this.start();
        } else if (!enabled && this.isListening) {
            this.stop();
        }
    }

    // Text-to-Speech methods
    speak(text, options = {}) {
        return new Promise((resolve, reject) => {
            if (!this.synthesis) {
                reject(new Error('Speech synthesis not supported'));
                return;
            }

            // Cancel any ongoing speech
            this.synthesis.cancel();

            const utterance = new SpeechSynthesisUtterance(text);
            utterance.rate = options.rate || this.voiceRate;
            utterance.pitch = options.pitch || this.voicePitch;
            utterance.volume = options.volume || 1.0;

            // Use selected voice or find a default
            if (this.selectedVoice) {
                utterance.voice = this.selectedVoice;
            } else {
                const voices = this.synthesis.getVoices();
                const defaultVoice = voices.find(v =>
                    v.lang.startsWith('en') && v.name.includes('Google')
                ) || voices.find(v => v.lang.startsWith('en'));
                if (defaultVoice) {
                    utterance.voice = defaultVoice;
                }
            }

            utterance.onend = () => {
                this.emit('speak_end');
                resolve();
            };

            utterance.onerror = (event) => {
                this.emit('speak_error', event);
                reject(event);
            };

            this.emit('speak_start', { text: text, voice: utterance.voice?.name });
            this.synthesis.speak(utterance);
        });
    }

    // Test the current voice
    testVoice() {
        this.speak("Hello, I am PIKA, your personal assistant.");
    }

    stopSpeaking() {
        if (this.synthesis) {
            this.synthesis.cancel();
        }
    }

    on(event, callback) {
        if (!this.listeners.has(event)) {
            this.listeners.set(event, []);
        }
        this.listeners.get(event).push(callback);
    }

    off(event, callback) {
        if (this.listeners.has(event)) {
            const listeners = this.listeners.get(event);
            const index = listeners.indexOf(callback);
            if (index > -1) {
                listeners.splice(index, 1);
            }
        }
    }

    emit(event, data) {
        if (this.listeners.has(event)) {
            this.listeners.get(event).forEach(callback => {
                try {
                    callback(data);
                } catch (error) {
                    console.error('Speech event listener error:', error);
                }
            });
        }
    }

    isSupported() {
        return !!(window.SpeechRecognition || window.webkitSpeechRecognition);
    }
}

// Global instance - uses native browser Web Speech API
window.pikaSpeech = new PikaSpeech();
