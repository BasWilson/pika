# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o pika ./cmd/pika

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates, tzdata, and ffmpeg for audio processing
RUN apk add --no-cache ca-certificates tzdata ffmpeg

# Copy binary from builder
COPY --from=builder /build/pika .

# Copy web assets
COPY --from=builder /build/web ./web

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./pika"]
