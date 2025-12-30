package server

import (
	"context"
	"net/http"
)

type contextKey string

const (
	formatKey contextKey = "response_format"
)

// ResponseFormat represents the desired response format
type ResponseFormat string

const (
	FormatHTMX ResponseFormat = "htmx"
	FormatJSON ResponseFormat = "json"
	FormatPush ResponseFormat = "push"
)

// ResponseFormatMiddleware detects and sets the response format
func ResponseFormatMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		format := detectFormat(r)
		ctx := context.WithValue(r.Context(), formatKey, format)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// detectFormat determines the response format from the request
func detectFormat(r *http.Request) ResponseFormat {
	// Check explicit header first
	if format := r.Header.Get("X-Response-Format"); format != "" {
		switch format {
		case "htmx":
			return FormatHTMX
		case "json":
			return FormatJSON
		case "push":
			return FormatPush
		}
	}

	// Check if it's an HTMX request
	if r.Header.Get("HX-Request") == "true" {
		return FormatHTMX
	}

	// Check Accept header
	accept := r.Header.Get("Accept")
	if accept == "application/json" {
		return FormatJSON
	}

	// Default to JSON for API requests, HTMX for browser
	if r.Header.Get("HX-Request") == "" && accept == "" {
		return FormatJSON
	}

	return FormatHTMX
}

// GetFormat retrieves the response format from context
func GetFormat(ctx context.Context) ResponseFormat {
	if format, ok := ctx.Value(formatKey).(ResponseFormat); ok {
		return format
	}
	return FormatJSON
}

// CORS middleware for development
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Response-Format, HX-Request, HX-Target, HX-Trigger")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging for static files
		if len(r.URL.Path) > 8 && r.URL.Path[:8] == "/static/" {
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}
