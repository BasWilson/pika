// PIKA Main Application - JARVIS Interface

class PikaApp {
    constructor() {
        this.ws = window.pikaWs;
        this.speech = window.pikaSpeech;
        this.isProcessing = false;
        this.hideCharacterTimeout = null;
        this.sleepTimeout = null;
        this.sleepDelay = 30000; // 30 seconds

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
        this.showGreeting();
        this.resetSleepTimer();
    }

    showGreeting() {
        // Show a friendly greeting when the app opens
        const greetings = [
            { text: "Hey there! What can I help you with today?", emotion: "happy" },
            { text: "Pika pika! Ready to assist!", emotion: "excited" },
            { text: "Hello! I'm here whenever you need me.", emotion: "helpful" },
            { text: "Good to see you! Ask me anything.", emotion: "happy" },
        ];
        const greeting = greetings[Math.floor(Math.random() * greetings.length)];

        // Set emotion briefly then go to idle
        this.setPikaEmotion(greeting.emotion);

        // Add greeting message and speak it after a short delay
        setTimeout(() => {
            this.addPikaMessage(greeting.text, greeting.emotion);
            this.speech.speak(greeting.text).then(() => {
                this.setPikaEmotion('idle');
            }).catch(() => {
                this.setPikaEmotion('idle');
            });
        }, 500);
    }

    resetSleepTimer() {
        // Clear existing sleep timeout
        if (this.sleepTimeout) {
            clearTimeout(this.sleepTimeout);
        }

        // Wake up if sleeping
        if (this.pikaAvatar && this.pikaAvatar.dataset.emotion === 'sleeping') {
            this.setPikaEmotion('idle');
        }

        // Start new sleep timer
        this.sleepTimeout = setTimeout(() => {
            this.fallAsleep();
        }, this.sleepDelay);
    }

    fallAsleep() {
        if (this.pikaAvatar && !this.isProcessing) {
            this.setPikaEmotion('sleeping');
        }
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
        this.pikaCharacter = document.getElementById('pika-character');
        this.pikaAvatar = this.pikaCharacter?.querySelector('.pika-avatar');
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

            // Reset sleep timer on interaction
            this.resetSleepTimer();

            // Show the message
            if (payload.text) {
                this.addPikaMessage(payload.text, payload.emotion);
            }

            // Show character and set emotion
            this.showPikaCharacter(payload.emotion || 'helpful');

            // Speak the response
            if (payload.text) {
                this.setOrbState('speaking');
                this.setPikaCharacterSpeaking(true);
                this.speech.speak(payload.text).then(() => {
                    this.setOrbState('idle');
                    this.setPikaCharacterSpeaking(false);
                    this.hidePikaCharacterDelayed();
                }).catch(() => {
                    this.setOrbState('idle');
                    this.setPikaCharacterSpeaking(false);
                    this.hidePikaCharacterDelayed();
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
            this.addErrorMessage('Microphone access denied. Please allow access in System Settings > Privacy & Security > Microphone.');
        });

        this.speech.on('unsupported', () => {
            this.addErrorMessage('Speech recognition not supported. Please use Chrome or Edge.');
        });

        this.speech.on('network_error', () => {
            this.addErrorMessage('Speech recognition requires internet (uses Google servers). Check your connection and try again.');
            this.setOrbState('idle');
            this.setStatus('Ready');
        });

        // Training events
        this.speech.on('training_started', () => {
            this.setStatus('Training');
            this.setOrbState('listening');
        });

        this.speech.on('training_sample', (data) => {
            this.updateTrainingProgress(data);
        });

        this.speech.on('training_complete', (data) => {
            this.onTrainingComplete(data);
        });

        this.speech.on('training_stopped', () => {
            this.setStatus('Ready');
            this.setOrbState('idle');
        });

        this.speech.on('wake_words_reset', () => {
            this.updateWakeWordsList();
        });

        this.speech.on('wake_word_added', () => {
            this.updateWakeWordsList();
        });

        this.speech.on('wake_word_removed', () => {
            this.updateWakeWordsList();
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
        this.resetSleepTimer();

        this.isProcessing = true;
        this.setStatus('Processing');
        this.setOrbState('processing');
        this.setPikaEmotion('thinking');

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

    // PIKA Character Control
    showPikaCharacter(emotion = 'helpful') {
        if (this.pikaCharacter) {
            this.pikaCharacter.classList.add('active');
            if (this.pikaAvatar) {
                this.pikaAvatar.dataset.emotion = emotion;
            }
        }
        // Clear any pending hide timeout
        if (this.hideCharacterTimeout) {
            clearTimeout(this.hideCharacterTimeout);
            this.hideCharacterTimeout = null;
        }
    }

    hidePikaCharacter() {
        if (this.pikaCharacter) {
            this.pikaCharacter.classList.remove('speaking');
            if (this.pikaAvatar) {
                this.pikaAvatar.classList.remove('speaking');
                this.pikaAvatar.dataset.emotion = 'idle';
            }
        }
    }

    hidePikaCharacterDelayed(delay = 3000) {
        if (this.hideCharacterTimeout) {
            clearTimeout(this.hideCharacterTimeout);
        }
        this.hideCharacterTimeout = setTimeout(() => {
            this.hidePikaCharacter();
        }, delay);
    }

    setPikaCharacterSpeaking(speaking) {
        if (this.pikaCharacter) {
            if (speaking) {
                this.pikaCharacter.classList.add('speaking');
                if (this.pikaAvatar) {
                    this.pikaAvatar.classList.add('speaking');
                }
            } else {
                this.pikaCharacter.classList.remove('speaking');
                if (this.pikaAvatar) {
                    this.pikaAvatar.classList.remove('speaking');
                }
            }
        }
    }

    setPikaEmotion(emotion) {
        if (this.pikaAvatar) {
            this.pikaAvatar.dataset.emotion = emotion;
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
        const actionType = payload.action_type;

        // Handle query actions with rich display
        if (success && actionType === 'GET_WEATHER' && payload.data) {
            this.displayWeatherResult(payload.data);
            return;
        }

        if (success && actionType === 'SEARCH_POKEMON' && payload.data) {
            this.displayPokemonResult(payload.data);
            return;
        }

        // Handle stop listening action
        if (success && actionType === 'STOP_LISTENING') {
            this.handleStopListening();
            return;
        }

        // Handle Google Calendar connected
        if (success && actionType === 'GOOGLE_CONNECTED') {
            this.handleGoogleConnected();
            return;
        }

        // Handle list reminders
        if (success && actionType === 'LIST_REMINDERS') {
            this.displayRemindersResult(payload.data);
            return;
        }

        // Handle game actions
        if (success && actionType === 'START_GAME' && payload.data) {
            this.displayGameUI(payload.data);
            return;
        }

        if (success && actionType === 'GAME_MOVE' && payload.data) {
            this.updateGameUI(payload.data);
            return;
        }

        // Default action message for other types
        const borderColor = success ? 'border-green-500/30' : 'border-red-500/30';
        const textColor = success ? 'text-green-400' : 'text-red-400';
        const message = success ? 'Action completed' : (payload.error || 'Action failed');

        const html = `
            <div class="flex justify-start">
                <div class="max-w-md">
                    <div class="text-xs ${textColor}/60 mb-1 font-mono uppercase tracking-wider">${this.escapeHtml(actionType)}</div>
                    <div class="bg-black/30 border ${borderColor} rounded-lg px-4 py-2">
                        <p class="text-gray-400 text-sm font-light">${this.escapeHtml(message)}</p>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);
    }

    displayWeatherResult(data) {
        const html = `
            <div class="flex justify-start">
                <div class="max-w-md w-full">
                    <div class="text-xs text-blue-400/60 mb-1 font-mono uppercase tracking-wider">Weather</div>
                    <div class="bg-gradient-to-br from-blue-900/30 to-blue-800/20 border border-blue-500/30 rounded-lg px-4 py-3">
                        <div class="flex items-center justify-between mb-2">
                            <span class="text-blue-300 font-medium">${this.escapeHtml(data.location)}</span>
                            <span class="text-2xl">${this.getWeatherEmoji(data.description)}</span>
                        </div>
                        <div class="text-3xl text-white font-light mb-1">${Math.round(data.temperature)}°C</div>
                        <div class="text-sm text-blue-200/80">${this.escapeHtml(data.description)}</div>
                        <div class="flex gap-4 mt-2 text-xs text-blue-300/60">
                            <span>Feels like ${Math.round(data.feels_like)}°C</span>
                            <span>Humidity ${data.humidity}%</span>
                            <span>Wind ${Math.round(data.wind_speed)} km/h</span>
                        </div>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);

        // Speak the weather summary
        const speechText = `It's currently ${Math.round(data.temperature)} degrees celsius in ${data.location}, with ${data.description.toLowerCase()}. Feels like ${Math.round(data.feels_like)} degrees.`;
        this.speech.speak(speechText);
    }

    displayPokemonResult(data) {
        const types = data.types ? data.types.join(', ') : 'Unknown';
        const abilities = data.abilities ? data.abilities.slice(0, 3).join(', ') : 'Unknown';

        const html = `
            <div class="flex justify-start">
                <div class="max-w-md w-full">
                    <div class="text-xs text-yellow-400/60 mb-1 font-mono uppercase tracking-wider">Pokemon #${data.id}</div>
                    <div class="bg-gradient-to-br from-yellow-900/30 to-orange-800/20 border border-yellow-500/30 rounded-lg px-4 py-3">
                        <div class="flex items-start gap-4">
                            ${data.sprite ? `<img src="${data.sprite}" alt="${data.name}" class="w-20 h-20 pixelated" style="image-rendering: pixelated;">` : ''}
                            <div class="flex-1">
                                <div class="text-xl text-white font-medium mb-1">${this.escapeHtml(data.name)}</div>
                                <div class="text-sm text-yellow-200/80 mb-2">${this.escapeHtml(types)}</div>
                                <div class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-yellow-300/60">
                                    <span>Height: ${data.height_m}m</span>
                                    <span>Weight: ${data.weight_kg}kg</span>
                                </div>
                                <div class="mt-2 text-xs text-yellow-300/60">
                                    <span>Abilities: ${this.escapeHtml(abilities)}</span>
                                </div>
                            </div>
                        </div>
                        ${data.stats ? this.renderPokemonStats(data.stats) : ''}
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);

        // Speak the pokemon summary
        const speechText = `${data.name} is a ${types} type Pokemon. It's ${data.height_m} meters tall and weighs ${data.weight_kg} kilograms.`;
        this.speech.speak(speechText);
    }

    renderPokemonStats(stats) {
        const statNames = { hp: 'HP', attack: 'ATK', defense: 'DEF', 'special-attack': 'SP.ATK', 'special-defense': 'SP.DEF', speed: 'SPD' };
        const maxStat = 255;

        let html = '<div class="mt-3 space-y-1">';
        for (const [key, value] of Object.entries(stats)) {
            const name = statNames[key] || key;
            const percent = (value / maxStat) * 100;
            html += `
                <div class="flex items-center gap-2 text-xs">
                    <span class="w-12 text-yellow-300/60">${name}</span>
                    <div class="flex-1 h-1.5 bg-black/30 rounded-full overflow-hidden">
                        <div class="h-full bg-yellow-400/60 rounded-full" style="width: ${percent}%"></div>
                    </div>
                    <span class="w-8 text-right text-yellow-300/60">${value}</span>
                </div>
            `;
        }
        html += '</div>';
        return html;
    }

    displayRemindersResult(data) {
        const reminders = data || [];

        if (reminders.length === 0) {
            const html = `
                <div class="flex justify-start">
                    <div class="max-w-md w-full">
                        <div class="text-xs text-purple-400/60 mb-1 font-mono uppercase tracking-wider">Reminders</div>
                        <div class="bg-gradient-to-br from-purple-900/30 to-purple-800/20 border border-purple-500/30 rounded-lg px-4 py-3">
                            <p class="text-purple-200/80 text-sm">No active reminders.</p>
                        </div>
                    </div>
                </div>
            `;
            this.appendMessage(html);
            this.speech.speak("You have no active reminders.");
            return;
        }

        // Build reminder list HTML
        let reminderListHtml = '';
        const speechParts = [];

        reminders.forEach((reminder, index) => {
            const timeStr = this.formatReminderTime(reminder.remind_at);
            reminderListHtml += `
                <div class="flex items-start gap-3 ${index > 0 ? 'mt-3 pt-3 border-t border-purple-500/20' : ''}">
                    <div class="text-purple-400 mt-0.5">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
                        </svg>
                    </div>
                    <div class="flex-1">
                        <div class="text-white font-medium text-sm">${this.escapeHtml(reminder.title)}</div>
                        <div class="text-purple-300/60 text-xs mt-0.5">${timeStr}</div>
                        ${reminder.description ? `<div class="text-purple-200/50 text-xs mt-1">${this.escapeHtml(reminder.description)}</div>` : ''}
                    </div>
                </div>
            `;
            speechParts.push(`${reminder.title}, ${timeStr}`);
        });

        const html = `
            <div class="flex justify-start">
                <div class="max-w-md w-full">
                    <div class="text-xs text-purple-400/60 mb-1 font-mono uppercase tracking-wider">Reminders (${reminders.length})</div>
                    <div class="bg-gradient-to-br from-purple-900/30 to-purple-800/20 border border-purple-500/30 rounded-lg px-4 py-3">
                        ${reminderListHtml}
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);

        // Speak the reminders
        const count = reminders.length;
        const intro = count === 1 ? "You have 1 reminder:" : `You have ${count} reminders:`;
        const speechText = `${intro} ${speechParts.join('. ')}`;
        this.speech.speak(speechText);
    }

    formatReminderTime(isoString) {
        try {
            const date = new Date(isoString);
            const now = new Date();
            const tomorrow = new Date(now);
            tomorrow.setDate(tomorrow.getDate() + 1);

            const timeOptions = { hour: 'numeric', minute: '2-digit', hour12: true };
            const timeStr = date.toLocaleTimeString('en-US', timeOptions);

            // Check if it's today
            if (date.toDateString() === now.toDateString()) {
                return `Today at ${timeStr}`;
            }

            // Check if it's tomorrow
            if (date.toDateString() === tomorrow.toDateString()) {
                return `Tomorrow at ${timeStr}`;
            }

            // Otherwise show full date
            const dateOptions = { weekday: 'short', month: 'short', day: 'numeric' };
            const dateStr = date.toLocaleDateString('en-US', dateOptions);
            return `${dateStr} at ${timeStr}`;
        } catch (e) {
            return isoString;
        }
    }

    // Store current game state for button interactions
    currentGameState = null;

    displayGameUI(data) {
        this.currentGameState = data;

        const html = `
            <div class="flex justify-start">
                <div class="max-w-md w-full">
                    <div class="flex items-center justify-between text-xs text-purple-400/60 mb-1 font-mono uppercase tracking-wider">
                        <span>Higher/Lower</span>
                        <span>Streak: ${data.streak} 🔥</span>
                    </div>
                    <div class="bg-gradient-to-br from-purple-900/30 to-purple-800/20 border border-purple-500/30 rounded-lg px-4 py-4">
                        <div class="text-center mb-4">
                            <div class="text-purple-300/60 text-sm mb-2">Is the next number...</div>
                            <div class="text-5xl font-bold text-white mb-2">${data.current_number}</div>
                        </div>
                        <div class="flex gap-3 justify-center mb-3">
                            <button onclick="makeGameMove('higher')" class="flex-1 bg-green-500/20 border border-green-500/30 text-green-400 rounded-lg px-4 py-3 text-sm font-mono uppercase tracking-wider hover:bg-green-500/30 transition-colors flex items-center justify-center gap-2">
                                <span>▲</span> Higher
                            </button>
                            <button onclick="makeGameMove('lower')" class="flex-1 bg-red-500/20 border border-red-500/30 text-red-400 rounded-lg px-4 py-3 text-sm font-mono uppercase tracking-wider hover:bg-red-500/30 transition-colors flex items-center justify-center gap-2">
                                <span>▼</span> Lower
                            </button>
                        </div>
                        <div class="text-center">
                            <button onclick="makeGameMove('quit')" class="text-xs text-gray-500 hover:text-gray-400 transition-colors">
                                Quit Game
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);

        // Speak the intro
        this.speech.speak(`I'm thinking of a number. The current number is ${data.current_number}. Is the next number higher or lower?`);
    }

    updateGameUI(data) {
        this.currentGameState = data;

        // Check if game is over
        if (data.game_over) {
            const html = `
                <div class="flex justify-start">
                    <div class="max-w-md w-full">
                        <div class="text-xs text-purple-400/60 mb-1 font-mono uppercase tracking-wider">Game Over</div>
                        <div class="bg-gradient-to-br from-purple-900/30 to-purple-800/20 border border-purple-500/30 rounded-lg px-4 py-4 text-center">
                            <div class="text-2xl mb-2">🎮</div>
                            <div class="text-white font-medium mb-1">Thanks for playing!</div>
                            <div class="text-purple-300/60 text-sm">Best streak: ${data.best_streak} 🔥</div>
                        </div>
                    </div>
                </div>
            `;
            this.appendMessage(html);
            this.speech.speak(`Game over! Your best streak was ${data.best_streak}.`);
            return;
        }

        // Show result and continue game
        const isCorrect = data.is_correct;
        const resultIcon = isCorrect ? '✓' : '✗';
        const resultColor = isCorrect ? 'text-green-400' : 'text-red-400';
        const resultBg = isCorrect ? 'bg-green-500/10' : 'bg-red-500/10';

        const html = `
            <div class="flex justify-start">
                <div class="max-w-md w-full">
                    <div class="flex items-center justify-between text-xs text-purple-400/60 mb-1 font-mono uppercase tracking-wider">
                        <span>Higher/Lower</span>
                        <span>Streak: ${data.streak} 🔥${data.best_streak > data.streak ? ` (Best: ${data.best_streak})` : ''}</span>
                    </div>
                    <div class="bg-gradient-to-br from-purple-900/30 to-purple-800/20 border border-purple-500/30 rounded-lg px-4 py-4">
                        <!-- Result Banner -->
                        <div class="${resultBg} rounded-lg px-3 py-2 mb-4 text-center">
                            <span class="${resultColor} font-bold text-lg">${resultIcon} ${isCorrect ? 'Correct!' : 'Wrong!'}</span>
                            <div class="text-gray-400 text-sm">${this.escapeHtml(data.message)}</div>
                        </div>

                        <div class="text-center mb-4">
                            <div class="text-purple-300/60 text-sm mb-2">Is the next number...</div>
                            <div class="text-5xl font-bold text-white mb-2">${data.current_number}</div>
                        </div>
                        <div class="flex gap-3 justify-center mb-3">
                            <button onclick="makeGameMove('higher')" class="flex-1 bg-green-500/20 border border-green-500/30 text-green-400 rounded-lg px-4 py-3 text-sm font-mono uppercase tracking-wider hover:bg-green-500/30 transition-colors flex items-center justify-center gap-2">
                                <span>▲</span> Higher
                            </button>
                            <button onclick="makeGameMove('lower')" class="flex-1 bg-red-500/20 border border-red-500/30 text-red-400 rounded-lg px-4 py-3 text-sm font-mono uppercase tracking-wider hover:bg-red-500/30 transition-colors flex items-center justify-center gap-2">
                                <span>▼</span> Lower
                            </button>
                        </div>
                        <div class="text-center">
                            <button onclick="makeGameMove('quit')" class="text-xs text-gray-500 hover:text-gray-400 transition-colors">
                                Quit Game
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        `;
        this.appendMessage(html);

        // Speak the result
        if (isCorrect) {
            this.speech.speak(`Correct! The number was ${data.current_number}. Your streak is ${data.streak}. Higher or lower?`);
        } else {
            this.speech.speak(`Wrong! The number was ${data.current_number}. Your streak resets. Higher or lower?`);
        }
    }

    getWeatherEmoji(description) {
        const desc = description.toLowerCase();
        if (desc.includes('clear') || desc.includes('sunny')) return '☀️';
        if (desc.includes('partly cloudy')) return '⛅';
        if (desc.includes('cloudy') || desc.includes('overcast')) return '☁️';
        if (desc.includes('rain') || desc.includes('drizzle')) return '🌧️';
        if (desc.includes('thunder')) return '⛈️';
        if (desc.includes('snow')) return '❄️';
        if (desc.includes('fog')) return '🌫️';
        return '🌤️';
    }

    handleStopListening() {
        // Turn off always-listen mode if it's on
        if (this.speech.alwaysListen) {
            this.speech.setAlwaysListen(false);

            // Update UI
            const statusEl = document.getElementById('always-listen-status');
            const btnEl = document.getElementById('always-listen-btn');

            if (statusEl) statusEl.textContent = 'OFF';
            if (btnEl) {
                btnEl.classList.remove('border-pika-400', 'text-pika-400', 'bg-pika-400/10');
                btnEl.classList.add('border-pika-400/30', 'text-pika-400/60');
            }
        }

        // Stop any active listening
        this.speech.stop();
        this.setOrbState('idle');
        this.setStatus('Sleeping');
    }

    handleGoogleConnected() {
        // Dismiss the calendar notice
        const notice = document.getElementById('calendar-notice');
        if (notice) {
            notice.classList.add('hidden');
            localStorage.setItem('pika_calendar_notice_dismissed', 'true');
        }
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

    // Training UI methods
    updateTrainingProgress(data) {
        const progressEl = document.getElementById('training-progress');
        const samplesEl = document.getElementById('training-samples');
        const countEl = document.getElementById('training-count');

        if (countEl) {
            countEl.textContent = `${data.count}/${data.required}`;
        }

        if (progressEl) {
            const percent = (data.count / data.required) * 100;
            progressEl.style.width = `${percent}%`;
        }

        if (samplesEl) {
            samplesEl.innerHTML = data.samples.map(s =>
                `<div class="text-sm text-gray-300 bg-black/30 rounded px-2 py-1">"${this.escapeHtml(s)}"</div>`
            ).join('');
        }
    }

    onTrainingComplete(data) {
        this.setStatus('Ready');
        this.setOrbState('idle');
        this.speech.stop();
        this.updateWakeWordsList();

        // Show completion message
        const instructionsEl = document.getElementById('training-instructions');
        if (instructionsEl) {
            instructionsEl.innerHTML = `<span class="text-green-400">Training complete! ${data.wakeWords.length} wake words saved.</span>`;
        }

        // Update button states
        const startBtn = document.getElementById('start-training-btn');
        const doneBtn = document.getElementById('done-training-btn');
        if (startBtn) startBtn.classList.remove('hidden');
        if (doneBtn) doneBtn.classList.add('hidden');
    }

    updateWakeWordsList() {
        const listEl = document.getElementById('current-wake-words');
        const countEl = document.getElementById('wake-word-count');

        if (listEl) {
            const words = this.speech.getWakeWords();

            // Update count
            if (countEl) {
                countEl.textContent = words.length;
            }

            // Render wake words with remove buttons (except for 'pika')
            listEl.innerHTML = words.map(w => {
                const isPika = w.toLowerCase() === 'pika';
                const isDefault = this.speech.isDefaultWakeWord(w);

                // Different styling for default vs custom wake words
                const bgClass = isDefault ? 'bg-pika-400/20' : 'bg-blue-400/20';
                const textClass = isDefault ? 'text-pika-400' : 'text-blue-400';

                if (isPika) {
                    // 'pika' cannot be removed
                    return `<span class="text-xs ${bgClass} ${textClass} rounded px-2 py-1 cursor-default" title="Primary wake word (cannot be removed)">${this.escapeHtml(w)}</span>`;
                } else {
                    // Other words can be clicked to remove
                    return `<span class="text-xs ${bgClass} ${textClass} rounded px-2 py-1 cursor-pointer hover:bg-red-400/30 hover:text-red-400 transition-colors group" onclick="removeWakeWord('${this.escapeHtml(w)}')" title="Click to remove">
                        ${this.escapeHtml(w)}
                        <span class="ml-1 opacity-0 group-hover:opacity-100">&times;</span>
                    </span>`;
                }
            }).join(' ');
        }
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
    window.pikaApp.resetSleepTimer();
    window.pikaApp.setStatus('Processing');
    window.pikaApp.setOrbState('processing');
    window.pikaApp.setPikaEmotion('thinking');

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

function toggleAlwaysListenInfo() {
    const panel = document.getElementById('always-listen-info');
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

// Custom modal functions (Wails doesn't support native confirm/alert)
function showModal(title, message, buttons) {
    const overlay = document.getElementById('modal-overlay');
    const titleEl = document.getElementById('modal-title');
    const messageEl = document.getElementById('modal-message');
    const buttonsEl = document.getElementById('modal-buttons');

    titleEl.textContent = title;
    messageEl.textContent = message;
    buttonsEl.innerHTML = '';

    buttons.forEach(btn => {
        const button = document.createElement('button');
        button.textContent = btn.text;
        button.className = btn.primary
            ? 'flex-1 bg-pika-400/20 border border-pika-400/30 text-pika-400 rounded px-4 py-2 text-sm font-mono uppercase tracking-wider hover:bg-pika-400/30 transition-colors'
            : 'flex-1 bg-gray-800 border border-gray-700 text-gray-300 rounded px-4 py-2 text-sm font-mono uppercase tracking-wider hover:bg-gray-700 transition-colors';
        if (btn.danger) {
            button.className = 'flex-1 bg-red-500/20 border border-red-500/30 text-red-400 rounded px-4 py-2 text-sm font-mono uppercase tracking-wider hover:bg-red-500/30 transition-colors';
        }
        button.onclick = () => {
            hideModal();
            if (btn.onClick) btn.onClick();
        };
        buttonsEl.appendChild(button);
    });

    overlay.classList.remove('hidden');
}

function hideModal() {
    document.getElementById('modal-overlay').classList.add('hidden');
}

function showAlert(title, message) {
    showModal(title, message, [
        { text: 'OK', primary: true }
    ]);
}

function showConfirm(title, message, onConfirm) {
    showModal(title, message, [
        { text: 'Cancel' },
        { text: 'Confirm', danger: true, onClick: onConfirm }
    ]);
}

function resetApp() {
    showConfirm(
        'Reset App',
        'This will clear all settings and quit the app.\n\nReopen PIKA to run the setup wizard.\n\nAre you sure?',
        async () => {
            try {
                const response = await fetch('http://localhost:8080/api/reset', { method: 'POST' });
                if (response.ok) {
                    localStorage.clear();
                    // Quit the app - user will reopen and see setup wizard
                    if (window.runtime && window.runtime.Quit) {
                        window.runtime.Quit();
                    } else {
                        showAlert('Reset Complete', 'Settings cleared. Please quit and reopen PIKA to run the setup wizard.');
                    }
                } else {
                    showAlert('Error', 'Failed to reset app. Please try again.');
                }
            } catch (error) {
                console.error('Reset failed:', error);
                showAlert('Error', 'Failed to reset app: ' + error.message);
            }
        }
    );
}

// Wake word training functions
function toggleTraining() {
    const panel = document.getElementById('training-panel');
    if (panel) {
        const isHidden = panel.classList.contains('hidden');
        panel.classList.toggle('hidden');

        if (isHidden) {
            // Panel is being opened - update wake words list
            window.pikaApp.updateWakeWordsList();
            resetTrainingUI();
        } else {
            // Panel is being closed - stop training if in progress
            if (window.pikaSpeech.isTraining) {
                window.pikaSpeech.stopTraining();
            }
        }
    }
}

function startTrainingSession() {
    // Reset UI
    resetTrainingUI();

    // Update instructions
    const instructionsEl = document.getElementById('training-instructions');
    if (instructionsEl) {
        instructionsEl.innerHTML = 'Say "PIKA" clearly...';
    }

    // Update button states
    const startBtn = document.getElementById('start-training-btn');
    const doneBtn = document.getElementById('done-training-btn');
    if (startBtn) startBtn.classList.add('hidden');
    if (doneBtn) doneBtn.classList.remove('hidden');

    // Start training
    window.pikaSpeech.startTraining();
}

function cancelTraining() {
    window.pikaSpeech.stopTraining();
    resetTrainingUI();
    toggleTraining();
}

function resetTrainingUI() {
    const progressEl = document.getElementById('training-progress');
    const samplesEl = document.getElementById('training-samples');
    const countEl = document.getElementById('training-count');
    const instructionsEl = document.getElementById('training-instructions');
    const startBtn = document.getElementById('start-training-btn');
    const doneBtn = document.getElementById('done-training-btn');

    if (progressEl) progressEl.style.width = '0%';
    if (samplesEl) samplesEl.innerHTML = '';
    if (countEl) countEl.textContent = '0/5';
    if (instructionsEl) instructionsEl.innerHTML = 'Click Start and say "PIKA" 5 times. Captures variations like "pick up", "peek a".';
    if (startBtn) startBtn.classList.remove('hidden');
    if (doneBtn) doneBtn.classList.add('hidden');
}

function resetWakeWords() {
    showConfirm('Reset Wake Words', 'Reset all wake words to defaults?', () => {
        window.pikaSpeech.resetWakeWords();
        resetTrainingUI();
        window.pikaApp.updateWakeWordsList();
    });
}

// Add wake word from text input
function addWakeWordFromInput() {
    const input = document.getElementById('wake-word-input');
    if (!input) return;

    const word = input.value.trim();
    if (word) {
        const added = window.pikaSpeech.addWakeWord(word);
        if (added) {
            input.value = '';
            window.pikaApp.updateWakeWordsList();
        } else {
            // Word already exists or invalid
            input.classList.add('border-red-500');
            setTimeout(() => input.classList.remove('border-red-500'), 1000);
        }
    }
}

// Remove a wake word
function removeWakeWord(word) {
    const removed = window.pikaSpeech.removeWakeWord(word);
    if (removed) {
        window.pikaApp.updateWakeWordsList();
    }
}

// Make a game move (Higher/Lower game) - calls API directly for reliable comparison
async function makeGameMove(move) {
    const state = window.pikaApp.currentGameState;
    if (!state) {
        console.error('No active game state');
        return;
    }

    // Show user's move
    window.pikaApp.addUserMessage(move);

    try {
        const response = await fetch('/api/game/move', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                move: move,
                game_state: state
            })
        });

        const result = await response.json();

        if (result.success && result.data) {
            // Update game UI with the result
            window.pikaApp.updateGameUI(result.data);
        } else {
            console.error('Game move failed:', result.error);
        }
    } catch (error) {
        console.error('Failed to make game move:', error);
    }
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
