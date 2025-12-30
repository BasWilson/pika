// WebSocket connection manager for PIKA

class PikaWebSocket {
    constructor() {
        this.ws = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.reconnectDelay = 1000;
        this.listeners = new Map();
        this.pendingRequests = new Map();
        this.connected = false;
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        console.log('Connecting to WebSocket:', wsUrl);
        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            console.log('WebSocket connected');
            this.connected = true;
            this.reconnectAttempts = 0;
            this.emit('connected');
        };

        this.ws.onclose = (event) => {
            console.log('WebSocket closed:', event.code, event.reason);
            this.connected = false;
            this.emit('disconnected');
            this.attemptReconnect();
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.emit('error', error);
        };

        this.ws.onmessage = (event) => {
            // Server may batch multiple JSON messages with newlines
            const messages = event.data.split('\n').filter(m => m.trim());
            messages.forEach(msg => this.handleMessage(msg));
        };
    }

    handleMessage(data) {
        try {
            const message = JSON.parse(data);
            console.log('Received message:', message.type, message);

            // Emit typed event
            this.emit(message.type, message);

            // Emit general message event
            this.emit('message', message);

            // Resolve pending request if applicable
            if (message.request_id && this.pendingRequests.has(message.request_id)) {
                const { resolve } = this.pendingRequests.get(message.request_id);
                resolve(message);
                this.pendingRequests.delete(message.request_id);
            }
        } catch (error) {
            console.error('Failed to parse message:', error);
        }
    }

    send(type, payload, format = 'htmx') {
        if (!this.connected) {
            console.error('WebSocket not connected');
            return Promise.reject(new Error('Not connected'));
        }

        const requestId = this.generateRequestId();
        const message = {
            type: type,
            payload: payload,
            request_id: requestId,
            format: format,
            timestamp: new Date().toISOString()
        };

        return new Promise((resolve, reject) => {
            this.pendingRequests.set(requestId, { resolve, reject });

            // Timeout after 30 seconds
            setTimeout(() => {
                if (this.pendingRequests.has(requestId)) {
                    this.pendingRequests.delete(requestId);
                    reject(new Error('Request timeout'));
                }
            }, 30000);

            this.ws.send(JSON.stringify(message));
        });
    }

    sendCommand(text, wakeWord = false, confidence = 1.0) {
        // Commands don't need request tracking - responses come via event handlers
        if (!this.connected) {
            return Promise.reject(new Error('Not connected'));
        }

        const message = {
            type: 'command',
            payload: {
                text: text,
                wake_word: wakeWord,
                confidence: confidence
            },
            request_id: this.generateRequestId(),
            format: 'htmx',
            timestamp: new Date().toISOString()
        };

        try {
            this.ws.send(JSON.stringify(message));
            return Promise.resolve();
        } catch (error) {
            return Promise.reject(error);
        }
    }

    sendStatus(status) {
        return this.send('status', {
            status: status,
            connected: this.connected
        });
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
                    console.error('Event listener error:', error);
                }
            });
        }
    }

    attemptReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            console.error('Max reconnection attempts reached');
            this.emit('reconnect_failed');
            return;
        }

        this.reconnectAttempts++;
        const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);

        console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
        this.emit('reconnecting', { attempt: this.reconnectAttempts, delay: delay });

        setTimeout(() => {
            this.connect();
        }, delay);
    }

    generateRequestId() {
        return 'req_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
    }

    disconnect() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
    }

    isConnected() {
        return this.connected;
    }
}

// Global instance
window.pikaWs = new PikaWebSocket();
