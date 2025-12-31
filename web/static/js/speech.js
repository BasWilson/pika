// Web Speech API wrapper for PIKA

class PikaSpeech {
    constructor() {
        this.recognition = null;
        this.synthesis = window.speechSynthesis;
        this.isListening = false;
        this.alwaysListen = false;
        // Default wake words - common mishearings/variations of "pika"
        this.defaultWakeWords = [
            'pika', 'peeka', 'pica', 'pick a', 'pick up', 'peak a',
            'peek a', 'picker', 'pikachu', 'pico', 'picka', 'peka',
            'heyPika', 'hey pika', 'ok pika', 'okay pika'
        ];
        this.wakeWords = [...this.defaultWakeWords];  // Array of valid wake words (trained variants)
        this.wakeWordDetected = false;
        this.listeners = new Map();
        this.currentTranscript = '';
        this.voices = [];
        this.selectedVoice = null;
        this.voiceRate = 1.0;
        this.voicePitch = 1.5;
        this.networkErrorCount = 0;
        this.maxNetworkRetries = 3;

        // Training mode state
        this.isTraining = false;
        this.trainingSamples = [];
        this.requiredSamples = 5;

        // Wake word timeout - how long to accept commands without wake word (ms)
        this.wakeWordTimeout = 30000; // 30 seconds
        this.wakeWordTimer = null;

        this.loadWakeWords();
        this.initRecognition();
        this.initVoices();
    }

    // Load saved wake words from localStorage
    loadWakeWords() {
        try {
            const saved = localStorage.getItem('pika_wake_words');
            if (saved) {
                const parsed = JSON.parse(saved);
                if (Array.isArray(parsed) && parsed.length > 0) {
                    // Merge saved with defaults, ensuring no duplicates
                    this.wakeWords = [...new Set([...this.defaultWakeWords, ...parsed])];
                }
            }
        } catch (error) {
            console.error('Failed to load wake words:', error);
            this.wakeWords = [...this.defaultWakeWords];
        }
    }

    // Save wake words to localStorage
    saveWakeWords() {
        try {
            localStorage.setItem('pika_wake_words', JSON.stringify(this.wakeWords));
        } catch (error) {
            console.error('Failed to save wake words:', error);
        }
    }

    // Start training mode
    startTraining() {
        this.isTraining = true;
        this.trainingSamples = [];
        this.emit('training_started');

        // Start listening if not already
        if (!this.isListening) {
            this.start();
        }
    }

    // Stop training mode without saving
    stopTraining() {
        this.isTraining = false;
        this.trainingSamples = [];
        this.emit('training_stopped');
    }

    // Finish training and save samples
    finishTraining() {
        if (this.trainingSamples.length > 0) {
            // Merge new samples with existing wake words, avoiding duplicates
            const allWords = [...this.wakeWords, ...this.trainingSamples];
            this.wakeWords = [...new Set(allWords.map(w => w.toLowerCase()))];
            this.saveWakeWords();
        }

        this.isTraining = false;
        this.trainingSamples = [];
        this.emit('training_complete', { wakeWords: this.wakeWords });
    }

    // Reset wake words to default
    resetWakeWords() {
        this.wakeWords = [...this.defaultWakeWords];
        this.saveWakeWords();
        this.emit('wake_words_reset');
    }

    // Get current wake words
    getWakeWords() {
        return [...this.wakeWords];
    }

    // Add a single wake word manually
    addWakeWord(word) {
        if (!word || typeof word !== 'string') return false;

        const normalized = word.trim().toLowerCase();
        if (normalized.length === 0) return false;

        if (!this.wakeWords.includes(normalized)) {
            this.wakeWords.push(normalized);
            this.saveWakeWords();
            this.emit('wake_word_added', { word: normalized });
            return true;
        }
        return false;
    }

    // Remove a single wake word
    removeWakeWord(word) {
        if (!word || typeof word !== 'string') return false;

        const normalized = word.trim().toLowerCase();

        // Prevent removing 'pika' - always keep at least one
        if (normalized === 'pika') return false;

        const index = this.wakeWords.indexOf(normalized);
        if (index > -1) {
            this.wakeWords.splice(index, 1);
            this.saveWakeWords();
            this.emit('wake_word_removed', { word: normalized });
            return true;
        }
        return false;
    }

    // Check if a word is a default wake word
    isDefaultWakeWord(word) {
        return this.defaultWakeWords.includes(word.toLowerCase());
    }

    // Activate wake word mode with timeout
    activateWakeWord() {
        this.wakeWordDetected = true;
        this.resetWakeWordTimer();
    }

    // Reset the wake word timeout timer
    resetWakeWordTimer() {
        if (this.wakeWordTimer) {
            clearTimeout(this.wakeWordTimer);
        }
        this.wakeWordTimer = setTimeout(() => {
            this.wakeWordDetected = false;
            this.wakeWordTimer = null;
            this.emit('wake_word_timeout');
            console.log('Wake word timeout - waiting for wake word again');
        }, this.wakeWordTimeout);
    }

    // Clear wake word state (e.g., when disabling always listen)
    clearWakeWordState() {
        if (this.wakeWordTimer) {
            clearTimeout(this.wakeWordTimer);
            this.wakeWordTimer = null;
        }
        this.wakeWordDetected = false;
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

            // Default to Samantha voice, fall back to other English voices
            if (!this.selectedVoice) {
                this.selectedVoice = this.voices.find(v => v.name === 'Samantha') ||
                    this.voices.find(v => v.name.includes('Samantha')) ||
                    this.voices.find(v => v.lang.startsWith('en') && (v.name.includes('Google') || v.name.includes('Daniel'))) ||
                    this.voices.find(v => v.lang.startsWith('en'));
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

        // Handle training mode
        if (this.isTraining) {
            this.handleTrainingSample(text);
            return;
        }

        const lowerText = text.toLowerCase();

        // Check for any wake word variant
        if (this.alwaysListen) {
            const matchedWakeWord = this.findMatchingWakeWord(lowerText);

            if (matchedWakeWord) {
                this.activateWakeWord();

                // Extract command after wake word using word boundary match
                const regex = new RegExp(`\\b${this.escapeRegex(matchedWakeWord)}\\b`, 'i');
                const match = regex.exec(lowerText);
                const command = match ? text.substring(match.index + matchedWakeWord.length).trim() : '';

                if (command) {
                    this.emit('command', {
                        text: command,
                        wakeWord: true,
                        confidence: confidence,
                        fullText: text
                    });
                    this.resetWakeWordTimer(); // Keep listening for more commands
                } else {
                    // Wake word only - wait for command
                    this.emit('wake_word_detected');
                }
            } else if (this.wakeWordDetected) {
                // Command following wake word (within timeout window)
                this.emit('command', {
                    text: text,
                    wakeWord: true,
                    confidence: confidence
                });
                this.resetWakeWordTimer(); // Reset timer for another command
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

    // Find which wake word matches in the text (as a standalone word)
    findMatchingWakeWord(lowerText) {
        for (const wakeWord of this.wakeWords) {
            const word = wakeWord.toLowerCase();
            // Use word boundary regex to avoid matching "pika" inside "pikachu"
            const regex = new RegExp(`\\b${this.escapeRegex(word)}\\b`, 'i');
            if (regex.test(lowerText)) {
                return word;
            }
        }
        return null;
    }

    // Escape special regex characters
    escapeRegex(string) {
        return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }

    // Handle a training sample
    handleTrainingSample(text) {
        if (!text || text.trim().length === 0) {
            return;
        }

        // Extract the first two words from the transcript (to catch "pick up", "peek a", etc.)
        const words = text.trim().split(/\s+/);
        const sample = words.slice(0, 2).join(' ').toLowerCase();

        // Only add if not empty and not already in samples
        if (sample && !this.trainingSamples.includes(sample)) {
            this.trainingSamples.push(sample);
        }

        this.emit('training_sample', {
            sample: sample,
            fullText: text,
            count: this.trainingSamples.length,
            required: this.requiredSamples,
            samples: [...this.trainingSamples]
        });

        // Auto-finish when we have enough samples
        if (this.trainingSamples.length >= this.requiredSamples) {
            this.finishTraining();
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
        } else if (!enabled) {
            this.clearWakeWordState();
            if (this.isListening) {
                this.stop();
            }
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

            // Replace "pika" variations with "peeka" for correct pronunciation
            const spokenText = text.replace(/pika/gi, 'peeka');

            const utterance = new SpeechSynthesisUtterance(spokenText);
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
        this.speak("Hello! I'm PIKA, your personal assistant. How can I help?");
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
