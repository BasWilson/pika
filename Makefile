.PHONY: dev dev-db dev-ollama dev-services dev-server build run stop clean test logs help backfill

# Default target
help:
	@echo "PIKA Development Commands"
	@echo "========================="
	@echo ""
	@echo "  make dev          - Start full dev environment (DB + Ollama + hot-reload server)"
	@echo "  make dev-db       - Start only database (PostgreSQL + pgvector)"
	@echo "  make dev-ollama   - Start only Ollama (local embeddings)"
	@echo "  make dev-services - Start all services (DB + Ollama)"
	@echo "  make dev-server   - Start Go server with hot reload (requires air)"
	@echo "  make build        - Build the Docker image"
	@echo "  make run          - Run full stack in Docker"
	@echo "  make stop         - Stop all containers"
	@echo "  make logs         - Tail logs from all containers"
	@echo "  make clean        - Remove containers and volumes"
	@echo "  make test         - Run tests"
	@echo "  make install      - Install development dependencies"
	@echo "  make backfill     - Generate embeddings for existing memories"
	@echo ""

# Install development dependencies
install:
	@echo "Installing air for hot reload..."
	go install github.com/air-verse/air@latest
	@echo "Done! Make sure ~/go/bin is in your PATH"

# Start database only (for development)
dev-db:
	docker compose up -d db
	@echo "Waiting for database to be ready..."
	@sleep 3
	@echo "Database ready at localhost:5432"

# Start Ollama for local embeddings
dev-ollama:
	docker compose up -d ollama
	@echo "Waiting for Ollama to be ready..."
	@sleep 5
	@echo "Pulling embedding model..."
	@curl -s http://localhost:11434/api/pull -d '{"name": "nomic-embed-text"}' > /dev/null || true
	@echo "Ollama ready at localhost:11434"

# Start all services (db, ollama)
dev-services:
	docker compose up -d db ollama
	@echo "Waiting for database..."
	@sleep 3
	@echo "Pulling Ollama embedding model..."
	@curl -s http://localhost:11434/api/pull -d '{"name": "nomic-embed-text"}' > /dev/null || true
	@echo "Services ready: DB :5432, Ollama :11434"

# Start Go server with hot reload
dev-server:
	@if ! command -v air &> /dev/null; then \
		echo "air not found. Run 'make install' first"; \
		exit 1; \
	fi
	air

# Full development environment
dev: dev-services dev-server

# Build Docker image
build:
	docker compose build

# Run full stack in Docker (production-like)
run:
	docker compose up -d
	@echo "PIKA running at http://localhost:8080"

# Stop all containers
stop:
	docker compose down

# View logs
logs:
	docker compose logs -f

# Clean up everything
clean:
	docker compose down -v
	rm -rf tmp/

# Run tests
test:
	go test -v ./...

# Database shell
db-shell:
	docker compose exec db psql -U pika -d pika

# Reset database
db-reset:
	docker compose down -v db
	docker compose up -d db
	@sleep 3
	@echo "Database reset complete"

# Backfill embeddings for existing memories
backfill:
	@echo "Backfilling embeddings for existing memories..."
	go run ./cmd/backfill/main.go
