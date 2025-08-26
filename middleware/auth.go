package middleware

import (
	"net/http"
	"os"
	"strings"
)

// Default API key for authentication - CHANGE THIS IN PRODUCTION!
// You can set the API_KEY environment variable to override this default
var API_KEY = getAPIKey()

func getAPIKey() string {
	if key := os.Getenv("API_KEY"); key != "" {
		return key
	}
	// Default key for development - MUST be changed in production
	return "your-secure-api-key-here"
}

// IsValidAPIKey checks if the provided API key is valid
func IsValidAPIKey(apiKey string) bool {
	return apiKey == API_KEY
}

// APIKeyMiddleware validates the API key from request headers
func APIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for OPTIONS (CORS preflight), health check, swagger docs, and WebSocket endpoints
		if r.Method == "OPTIONS" || r.URL.Path == "/" || r.URL.Path == "/health" ||
			strings.HasPrefix(r.URL.Path, "/notforhumans/") ||
			strings.HasPrefix(r.URL.Path, "/ws/") {
			next.ServeHTTP(w, r)
			return
		}

		// Get API key from header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.Header.Get("Authorization")
			// Remove "Bearer " prefix if present
			if strings.HasPrefix(apiKey, "Bearer ") {
				apiKey = strings.TrimPrefix(apiKey, "Bearer ")
			}
		}

		// Validate API key
		if !IsValidAPIKey(apiKey) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"Invalid or missing API key. Use X-API-Key header or Authorization: Bearer token"}`))
			return
		}

		// API key is valid, proceed
		next.ServeHTTP(w, r)
	})
}
