// PIKA Main Application - JARVIS Interface

class PikaApp {
    constructor() {
        this.ws = window.pikaWs;
        this.speech = window.pikaSpeech;
        this.isProcessing = false;

        this.init();
    }

    init() {
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', () => this.setup());
        } else {
            this.setup();
        }
    }

    setup() {
        this.bindElements();
        this.setupWebSocket();
        this.setupSpeech();
        this.checkSpeechSupport();
    }

    bindElements() {
        this.messagesContainer = document.getElementById('messages');
        this.statusText = document.getElementById('status-text');
        this.connectionDot = document.getElementById('connection-dot');
        this.orbButton = document.getElementById('orb-button');
        this.micIcon = document.getElementById('mic-icon');
        this.voiceVisualizer = document.getElementById('voice-visualizer');
        this.innerRing = document.getElementById('inner-ring');
        this.transcript = document.getElementById('transcript');
        this.transcriptText = document.getElementById('transcript-text');
        this.alwaysListenBtn = document.getElementById('always-listen-btn');
        this.alwaysListenStatus = document.getElementById('always-listen-status');
        this.textInput = document.getElementById('text-input');
        this.calendarNotice = document.getElementById('calendar-notice');
    }

    setupWebSocket() {
        this.ws.on('connected', () => {
            this.updateConnectionStatus(true);
            this.setStatus('Online');
        });

        this.ws.on('disconnected', () => {
            this.updateConnectionStatus(false);
            this.setStatus('Offline');
        });

        this.ws.on('reconnecting', (data) => {
            this.setStatus(`Reconnecting`);
        });

        this.ws.on('reconnect_failed', () => {
            this.setStatus('Connection Lost');
        });

        this.ws.on('status', (msg) => {
            const payload = msg.payload;
            if (payload.status) {
                this.setStatus(this.capitalizeFirst(payload.status));
            }
        });

        this.ws.on('response', (msg) => {
            const payload = msg.payload;

            // Show the message
            if (payload.text) {
                this.addPikaMessage(payload.text, payload.emotion);
            }

            // Speak the response
            if (payload.text) {
                this.setOrbState('speaking');
                this.speech.speak(payload.text).then(() => {
                    this.setOrbState('idle');
                }).catch(() => {
                    this.setOrbState('idle');
                });
            }

            this.isProcessing = false;
            this.setStatus('Ready');
        });

        this.ws.on('action', (msg) => {
            const payload = msg.payload;
            this.addActionMessage(payload);
        });

        this.ws.on('error', (msg) => {
            const payload = msg.payload;
            this.addErrorMessage(payload.message);
            this.isProcessing = false;
            this.setStatus('Ready');
            this.setOrbState('idle');
        });

        this.ws.on('trigger', (msg) => {
            const payload = msg.payload;
            this.handleTrigger(payload);
        });

        this.ws.connect();
    }

    setupSpeech() {
        this.speech.on('start', () => {
            this.setOrbState('listening');
            this.setStatus('Listening');
        });

        this.speech.on('end', () => {
            if (!this.speech.alwaysListen && !this.isProcessing) {
                this.setOrbState('idle');
                this.setStatus('Ready');
            }
        });

        this.speech.on('interim', (data) => {
            this.showTranscript(data.text);
        });

        this.speech.on('command', (data) => {
            this.handleVoiceCommand(data);
        });

        this.speech.on('wake_word_detected', () => {
            this.setStatus('Listening');
            this.pulseOrb();
        });

        this.speech.on('permission_denied', () => {
            this.addErrorMessage('Microphone access denied. Please allow microphone access.');
        });

        this.speech.on('unsupported', () => {
            this.addErrorMessage('Speech recognition not supported. Please use Chrome or Edge.');
        });

        this.speech.on('network_error', () => {
            this.addErrorMessage('Speech recognition requires internet (uses Google servers). Check your connection and try again.');
            this.setOrbState('idle');
            this.setStatus('Ready');
        });

        this.speech.on('speak_start', () => {
            this.setOrbState('speaking');
            this.setStatus('Speaking');
        });

        this.speech.on('speak_end', () => {
            if (!this.speech.isListening) {
                this.setOrbState('idle');
                this.setStatus('Ready');
            } else {
                this.setOrbState('listening');
                this.setStatus('Listening');
            }
        });
    }

    checkSpeechSupport() {
        if (!this.speech.isSupported()) {
            console.warn('Speech recognition not supported');
        }
    }

    handleVoiceCommand(data) {
        this.hideTranscript();
        this.addUserMessage(data.text);

        this.isProcessing = true;
        this.setStatus('Processing');
        this.setOrbState('processing');

        if (!this.speech.alwaysListen) {
            this.speech.stop();
        }

        this.ws.sendCommand(data.text, data.wakeWord, data.confidence)
            .catch(error => {
                console.error('Failed to send command:', error);
                this.addErrorMessage('Failed to send command. Please try again.');
                this.isProcessing = false;
                this.setStatus('Ready');
                this.setOrbState('idle');
            });
    }

    handleTrigger(payload) {
        this.addPikaMessage(`${payload.title}: ${payload.message}`, 'alert');
        if (payload.message) {
            this.speech.speak(payload.message);
        }
    }

    // Orb state management
    setOrbState(state) {
        if (!this.orbButton) return;

        // Reset classes
        this.orbButton.classList.remove('orb-glow-active');
        if (this.innerRing) {
            this.innerRing.classList.remove('ring-border-active');
        }

        // Hide/show elements
        if (this.micIcon) this.micIcon.classList.remove('hidden');
        if (this.voiceVisualizer) this.voiceVisualizer.classList.add('hidden');

        switch (state) {
            case 'listening':
                this.orbButton.classList.add('orb-glow-active');
                if (this.innerRing) this.innerRing.classList.add('ring-border-active');
                if (this.micIcon) this.micIcon.classList.add('hidden');
                if (this.voiceVisualizer) this.voiceVisualizer.classList.remove('hidden');
                break;

            case 'processing':
                this.orbButton.classList.add('orb-glow-active');
                this.orbButton.style.animation = 'pulse-glow 1s ease-in-out infinite';
                break;

            case 'speaking':
                this.orbButton.classList.add('orb-glow-active');
                if (this.innerRing) this.innerRing.classList.add('ring-border-active');
                break;

            case 'idle':
            default:
                this.orbButton.style.animation = '';
                break;
        }
    }

    pulseOrb() {
        if (this.orbButton) {
            this.orbButton.classList.add('orb-glow-active');
            setTimeout(() => {
                if (!this.speech.isListening) {
                    this.orbButton.classList.remove('orb-glow-active');
                }
            }, 500);
        }
    }

    // Message UI
    addUserMessage(text) {
        const html = `
            <div class="flex justify-end">
                <div class="max-w-md">
                    <div class="text-xs text-gray-500 text-right mb-1 font-mono uppercase tracking-wider">You</div>
                    <div class="bg-pika-400/10 border border-pika-400/30 rounded-lg px-4 py-2">
                        <p class="text-gray-200 font-light">${this.escapeHtml(text)}</p>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);
    }

    addPikaMessage(text, emotion = '') {
        const html = `
            <div class="flex justify-start">
                <div class="max-w-md">
                    <div class="text-xs text-pika-400/60 mb-1 font-mono uppercase tracking-wider">PIKA ${emotion ? `/ ${emotion}` : ''}</div>
                    <div class="bg-black/50 border border-gray-800 rounded-lg px-4 py-2">
                        <p class="text-gray-200 font-light">${this.escapeHtml(text)}</p>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);
    }

    addActionMessage(payload) {
        const success = payload.success;
        const borderColor = success ? 'border-green-500/30' : 'border-red-500/30';
        const textColor = success ? 'text-green-400' : 'text-red-400';
        const message = success ? 'Action completed' : (payload.error || 'Action failed');

        const html = `
            <div class="flex justify-start">
                <div class="max-w-md">
                    <div class="text-xs ${textColor}/60 mb-1 font-mono uppercase tracking-wider">${this.escapeHtml(payload.action_type)}</div>
                    <div class="bg-black/30 border ${borderColor} rounded-lg px-4 py-2">
                        <p class="text-gray-400 text-sm font-light">${this.escapeHtml(message)}</p>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);
    }

    addErrorMessage(text) {
        const html = `
            <div class="flex justify-start">
                <div class="max-w-md">
                    <div class="text-xs text-red-400/60 mb-1 font-mono uppercase tracking-wider">Error</div>
                    <div class="bg-red-500/5 border border-red-500/30 rounded-lg px-4 py-2">
                        <p class="text-red-300 text-sm font-light">${this.escapeHtml(text)}</p>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);
    }

    appendMessage(html) {
        if (this.messagesContainer) {
            this.messagesContainer.insertAdjacentHTML('beforeend', html);
            this.messagesContainer.scrollTop = this.messagesContainer.scrollHeight;
        }
    }

    setStatus(status) {
        if (this.statusText) {
            this.statusText.textContent = status.toUpperCase();
        }
    }

    updateConnectionStatus(connected) {
        if (this.connectionDot) {
            this.connectionDot.classList.remove('bg-green-400', 'bg-red-400', 'bg-gray-500');
            this.connectionDot.classList.add(connected ? 'bg-green-400' : 'bg-red-400');
        }
    }

    showTranscript(text) {
        if (this.transcript && this.transcriptText) {
            this.transcript.classList.remove('hidden');
            this.transcriptText.textContent = `"${text}"`;
        }
    }

    hideTranscript() {
        if (this.transcript) {
            this.transcript.classList.add('hidden');
        }
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    capitalizeFirst(str) {
        return str.charAt(0).toUpperCase() + str.slice(1);
    }
}

// Global functions
function toggleListening() {
    window.pikaSpeech.toggle();
}

function toggleAlwaysListen() {
    const speech = window.pikaSpeech;
    const newState = !speech.alwaysListen;
    speech.setAlwaysListen(newState);

    const statusEl = document.getElementById('always-listen-status');
    const btnEl = document.getElementById('always-listen-btn');

    if (statusEl) {
        statusEl.textContent = newState ? 'ON' : 'OFF';
    }

    if (btnEl) {
        if (newState) {
            btnEl.classList.add('border-pika-400', 'text-pika-400', 'bg-pika-400/10');
            btnEl.classList.remove('border-pika-400/30', 'text-pika-400/60');
        } else {
            btnEl.classList.remove('border-pika-400', 'text-pika-400', 'bg-pika-400/10');
            btnEl.classList.add('border-pika-400/30', 'text-pika-400/60');
        }
    }
}

function sendTextCommand(event) {
    event.preventDefault();

    const input = document.getElementById('text-input');
    const text = input.value.trim();

    if (!text) return;

    window.pikaApp.addUserMessage(text);
    window.pikaApp.setStatus('Processing');
    window.pikaApp.setOrbState('processing');

    window.pikaWs.sendCommand(text, false, 1.0)
        .catch(error => {
            console.error('Failed to send command:', error);
            window.pikaApp.addErrorMessage('Failed to send command. Please try again.');
            window.pikaApp.setOrbState('idle');
        });

    input.value = '';
}

function dismissCalendarNotice() {
    const notice = document.getElementById('calendar-notice');
    if (notice) {
        notice.classList.add('hidden');
        localStorage.setItem('pika_calendar_notice_dismissed', 'true');
    }
}

// Voice settings functions
function toggleSettings() {
    const panel = document.getElementById('settings-panel');
    if (panel) {
        panel.classList.toggle('hidden');
    }
}

function populateVoiceSelect() {
    const select = document.getElementById('voice-select');
    const voices = window.pikaSpeech.getVoices();

    if (!select || voices.length === 0) return;

    // Group voices by language
    const grouped = {};
    voices.forEach(voice => {
        const lang = voice.lang.split('-')[0];
        if (!grouped[lang]) grouped[lang] = [];
        grouped[lang].push(voice);
    });

    // Build options HTML
    let html = '';

    // English voices first
    if (grouped['en']) {
        html += '<optgroup label="English">';
        grouped['en'].forEach(v => {
            const selected = window.pikaSpeech.selectedVoice?.name === v.name ? 'selected' : '';
            html += `<option value="${v.name}" ${selected}>${v.name}</option>`;
        });
        html += '</optgroup>';
    }

    // Other languages
    Object.keys(grouped).sort().forEach(lang => {
        if (lang === 'en') return;
        const langName = new Intl.DisplayNames(['en'], { type: 'language' }).of(lang) || lang;
        html += `<optgroup label="${langName}">`;
        grouped[lang].forEach(v => {
            const selected = window.pikaSpeech.selectedVoice?.name === v.name ? 'selected' : '';
            html += `<option value="${v.name}" ${selected}>${v.name}</option>`;
        });
        html += '</optgroup>';
    });

    select.innerHTML = html;
}

function changeVoice(voiceName) {
    window.pikaSpeech.setVoice(voiceName);
}

function changeRate(value) {
    const rate = parseFloat(value);
    window.pikaSpeech.setRate(rate);
    document.getElementById('rate-value').textContent = rate.toFixed(1);
}

function changePitch(value) {
    const pitch = parseFloat(value);
    window.pikaSpeech.setPitch(pitch);
    document.getElementById('pitch-value').textContent = pitch.toFixed(1);
}

function testVoice() {
    window.pikaSpeech.testVoice();
}

// Load saved settings
function loadVoiceSettings() {
    const savedRate = localStorage.getItem('pika_voice_rate');
    const savedPitch = localStorage.getItem('pika_voice_pitch');

    if (savedRate) {
        const rate = parseFloat(savedRate);
        window.pikaSpeech.voiceRate = rate;
        const rateSlider = document.getElementById('voice-rate');
        const rateValue = document.getElementById('rate-value');
        if (rateSlider) rateSlider.value = rate;
        if (rateValue) rateValue.textContent = rate.toFixed(1);
    }

    if (savedPitch) {
        const pitch = parseFloat(savedPitch);
        window.pikaSpeech.voicePitch = pitch;
        const pitchSlider = document.getElementById('voice-pitch');
        const pitchValue = document.getElementById('pitch-value');
        if (pitchSlider) pitchSlider.value = pitch;
        if (pitchValue) pitchValue.textContent = pitch.toFixed(1);
    }
}

// Initialize
window.pikaApp = new PikaApp();

// Setup voice selector when voices are loaded
window.pikaSpeech.on('voices_loaded', () => {
    populateVoiceSelect();
    loadVoiceSettings();
});

// Also try immediately in case voices are already loaded
setTimeout(() => {
    populateVoiceSelect();
    loadVoiceSettings();
}, 500);

// Check calendar status
async function checkCalendarAndShowNotice() {
    if (localStorage.getItem('pika_calendar_notice_dismissed')) return;

    try {
        const response = await fetch('/api/status');
        const status = await response.json();

        if (!status.calendar_connected) {
            const notice = document.getElementById('calendar-notice');
            if (notice) notice.classList.remove('hidden');
        }
    } catch (error) {
        console.error('Failed to check calendar status:', error);
    }
}

setTimeout(checkCalendarAndShowNotice, 2000);
