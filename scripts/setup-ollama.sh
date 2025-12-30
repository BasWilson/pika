#!/bin/bash
# Setup script to pull the embedding model for Ollama

set -e

OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
EMBED_MODEL="${OLLAMA_EMBED_MODEL:-nomic-embed-text}"

echo "Waiting for Ollama to be ready..."
until curl -s "$OLLAMA_URL/api/tags" > /dev/null 2>&1; do
    sleep 1
done

echo "Ollama is ready. Pulling embedding model: $EMBED_MODEL"
curl -s "$OLLAMA_URL/api/pull" -d "{\"name\": \"$EMBED_MODEL\"}" | while read -r line; do
    status=$(echo "$line" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$status" ]; then
        echo "  $status"
    fi
done

echo "Model $EMBED_MODEL is ready!"
