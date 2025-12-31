# PIKA

**Personal Intelligence Assistant** - A voice-controlled AI assistant for your Mac desktop.

PIKA is a native macOS application that lets you interact with AI using your voice. Just say "Pika" to activate, ask questions, manage your calendar, save memories, and more.

## Features

### Core
- **Voice Activation** - Say "Pika" to wake the assistant, or use continuous listening mode
- **Customizable Wake Words** - Add your own wake words, train with voice samples, or type custom phrases
- **AI-Powered Responses** - Natural conversations powered by your choice of AI model (Gemini, GPT-4, Claude, Llama, and more via Requesty.ai)
- **Natural Actions** - Just speak naturally - PIKA understands intent and takes action

### Productivity
- **Google Calendar Integration** - Add, edit, and delete calendar events with voice commands
- **Reminders** - Create reminders with multi-tier notifications (24h, 12h, 3h, 1h, 10min before, and at time)
- **Memory System** - PIKA remembers important information you tell it, with semantic search to recall relevant context

### Information
- **Weather Updates** - Get current weather for any location
- **Pokemon Search** - Look up Pokemon stats and information

### Entertainment
- **Higher/Lower Game** - Play a number guessing game with streak tracking

## System Requirements

- **macOS 10.15** (Catalina) or later
- **Apple Silicon Mac** (M1, M2, M3, or M4)
- Microphone access for voice commands
- Internet connection for AI responses

> **Note**: PIKA currently only supports Apple Silicon Macs. Intel Mac support may be added in the future.

## Installation

### Download

1. Go to the [Releases](../../releases) page
2. Download one of the following:
   - **`PIKA-*-lite.zip`** (~18MB) - Lighter download, requires Ollama installed separately
   - **`PIKA-*-full.zip`** (~150MB) - Includes bundled Ollama for offline AI embeddings

3. Extract the zip and drag `PIKA.app` to your Applications folder

### First Launch

Since PIKA is not signed with an Apple Developer certificate, macOS will block it by default:

1. Right-click (or Control-click) on `PIKA.app`
2. Select **"Open"** from the context menu
3. Click **"Open"** in the dialog that appears

You only need to do this once. After that, you can open PIKA normally.

### Lite Version: Install Ollama

If you downloaded the lite version, install Ollama for the memory/embedding system:

```bash
# Install via Homebrew
brew install ollama
```

Or download from [ollama.ai](https://ollama.ai)

PIKA will automatically start Ollama when needed.

## Setup

When you first launch PIKA, a setup wizard will guide you through configuration:

### 1. AI Provider (Required)

PIKA uses [Requesty.ai](https://requesty.ai) as a unified gateway to multiple AI providers.

1. Create a free account at [requesty.ai](https://requesty.ai)
2. Generate an API key
3. Enter it in the setup wizard

Requesty supports models from:
- Google (Gemini 2.0 Flash, Gemini Pro)
- OpenAI (GPT-4o, GPT-4o Mini)
- Anthropic (Claude 3.5 Sonnet, Claude 3 Opus)
- Meta (Llama 3.1, Llama 3.2)
- And many more

### 2. Google Calendar (Optional)

To enable calendar integration:

1. Go to [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Create a new project (or select existing)
3. Enable the Google Calendar API
4. Create OAuth 2.0 credentials (Desktop app type)
5. Enter the Client ID and Client Secret in PIKA's setup

### 3. Embedding Model

PIKA uses Ollama to run a local embedding model for the memory system. The setup wizard will:
- Start Ollama (bundled or system-installed)
- Pull the `nomic-embed-text` model (~274MB)

This runs locally - no data is sent to external servers for embeddings.

## How It Works

### Architecture

PIKA is built with:
- **[Wails](https://wails.io)** - Go + WebView framework for native macOS apps
- **Go backend** - Handles AI communication, calendar sync, memory storage
- **Embedded web UI** - Modern interface with voice visualization
- **SQLite database** - Local storage for configuration, memories, and cache

All data is stored locally in `~/Library/Application Support/PIKA/`.

### Voice Commands

1. **Wake Word**: Say "Pika" to activate listening
2. **Continuous Mode**: Toggle continuous listening in settings
3. **Speech Recognition**: Uses your Mac's built-in speech recognition (Web Speech API)
4. **Text-to-Speech**: Responses are spoken aloud (can be disabled)

### Actions

PIKA can understand your intent and take actions:

| Command Example | Action |
|-----------------|--------|
| "Add a meeting with John tomorrow at 3pm" | Creates calendar event |
| "Edit my 3pm meeting to 4pm" | Updates calendar event |
| "Delete the meeting with John" | Removes calendar event |
| "Remind me to call mom tomorrow at 9am" | Creates a reminder |
| "What reminders do I have?" | Lists active reminders |
| "I called mom" / "Delete the reminder" | Completes or removes reminder |
| "Remember that my wifi password is..." | Saves to memory |
| "What did I tell you about...?" | Searches memories |
| "What's the weather in Tokyo?" | Fetches weather |
| "Tell me about Pikachu" | Looks up Pokemon info |
| "Let's play a game" | Starts Higher/Lower game |
| "Goodbye PIKA" / "Stop listening" | Stops active listening mode |

### Memory System

PIKA uses vector embeddings to store and retrieve memories semantically:
- Important information is automatically extracted and saved
- When you ask questions, relevant memories are retrieved as context
- Embeddings are generated locally using Ollama (no data leaves your machine)

## Development

### Prerequisites

- Go 1.24 or later
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)

```bash
# Install Wails
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Verify installation
wails doctor
```

### Running Locally

```bash
# Clone the repository
git clone https://github.com/BasWilson/pika.git
cd pika

# Run in development mode (HTTP server)
make dev

# Or with hot reload
make install    # Install air for hot reload
make dev-server
```

Open http://localhost:8080 in your browser.

### Building

```bash
# Build for Apple Silicon
make build

# Optionally bundle Ollama into the app
make bundle-ollama

# The app will be at: build/bin/PIKA.app
```

### Project Structure

```
pika/
├── main.go              # Wails app entry point
├── internal/
│   ├── server/          # HTTP server & API routes
│   ├── ai/              # AI service (Requesty + Ollama)
│   ├── calendar/        # Google Calendar integration
│   ├── reminder/        # Reminders with scheduled notifications
│   ├── memory/          # Vector memory store
│   ├── actions/         # Action handlers (calendar, weather, games, etc.)
│   └── config/          # Configuration management
├── web/
│   ├── templates/       # HTML templates
│   └── static/          # CSS & JavaScript
└── build/               # macOS app bundle output
```

## Privacy

- **Local-first**: All data stored in `~/Library/Application Support/PIKA/`
- **Embeddings**: Generated locally via Ollama - no data sent externally
- **AI Responses**: Sent to your configured AI provider (via Requesty.ai)
- **Calendar**: Synced with Google Calendar if you enable integration
- **No telemetry**: PIKA does not collect usage data

## Troubleshooting

### "App is damaged and can't be opened"

Run this command to remove the quarantine flag:
```bash
xattr -cr /Applications/PIKA.app
```

### Microphone not working

1. Open System Preferences > Privacy & Security > Microphone
2. Ensure PIKA has permission

### Ollama not starting

If using the lite version:
```bash
# Check if Ollama is installed
which ollama

# Start manually if needed
ollama serve
```

### Calendar not syncing

1. Check Google OAuth credentials in setup
2. Re-authorize by clicking "Connect Google Calendar" in settings

## License

MIT License - See [LICENSE](LICENSE) for details.

---

Built with [Wails](https://wails.io) and [Requesty.ai](https://requesty.ai)
