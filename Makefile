.PHONY: help dev dev-server build build-intel test clean install install-wails bundle-ollama

# Default target
help:
	@echo "PIKA Development Commands"
	@echo "========================="
	@echo ""
	@echo "Development:"
	@echo "  make dev            - Start HTTP server in dev mode (open browser)"
	@echo "  make dev-server     - Start HTTP server with hot reload (air)"
	@echo "  make test           - Run tests"
	@echo ""
	@echo "Build:"
	@echo "  make build          - Build macOS desktop app (Apple Silicon)"
	@echo "  make build-intel    - Build macOS desktop app (Intel)"
	@echo "  make bundle-ollama  - Download Ollama into app bundle"
	@echo ""
	@echo "Setup:"
	@echo "  make install        - Install all development dependencies"
	@echo "  make install-wails  - Install Wails CLI"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean          - Remove build artifacts"
	@echo ""

# ============================================
# Development Commands
# ============================================

# Start HTTP server in dev mode
dev:
	@echo "Starting PIKA in dev mode..."
	@echo "Open http://localhost:8080 in your browser"
	@echo "Press Ctrl+C to stop."
	go run ./cmd/server

# Start HTTP server with hot reload using air
dev-server:
	@if ! command -v air &> /dev/null; then \
		echo "air not found. Run 'make install' first"; \
		exit 1; \
	fi
	air

# ============================================
# Build Commands
# ============================================

# Install Wails CLI
install-wails:
	@echo "Installing Wails CLI..."
	go install github.com/wailsapp/wails/v2/cmd/wails@latest
	@echo "Done! Run 'wails doctor' to verify installation"

# Build macOS desktop app (Apple Silicon)
build:
	@echo "Building PIKA desktop app..."
	~/go/bin/wails build -platform darwin/arm64 -skipbindings
	@echo ""
	@echo "Build complete: build/bin/PIKA.app"
	@echo "Run 'make bundle-ollama' to include Ollama in the app bundle"

# Build for Intel Mac
build-intel:
	@echo "Building PIKA desktop app (Intel)..."
	~/go/bin/wails build -platform darwin/amd64 -skipbindings
	@echo "Build complete: build/bin/PIKA.app"

# Download and bundle Ollama into the app
bundle-ollama:
	@echo "Downloading Ollama binary..."
	@mkdir -p build/bin/PIKA.app/Contents/Resources/ollama
	curl -L -o build/bin/PIKA.app/Contents/Resources/ollama/ollama \
		"https://github.com/ollama/ollama/releases/download/v0.5.4/ollama-darwin"
	chmod +x build/bin/PIKA.app/Contents/Resources/ollama/ollama
	@echo "Ollama bundled successfully!"
	@echo "App size:"
	@du -sh build/bin/PIKA.app

# ============================================
# Common Commands
# ============================================

# Install all development dependencies
install: install-wails
	@echo "Installing air for hot reload..."
	go install github.com/air-verse/air@latest
	@echo ""
	@echo "All dependencies installed!"
	@echo "Run 'make dev' to start the server in dev mode"

# Run tests
test:
	go test -v ./...

# Clean up everything
clean:
	rm -rf tmp/
	rm -rf build/bin/
	@echo "Clean complete"
