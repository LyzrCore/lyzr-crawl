package middleware

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture response data
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker for WebSocket support
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// LoggingMiddleware logs all HTTP requests and responses
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Check if this is a WebSocket upgrade request
		isWebSocket := r.Header.Get("Upgrade") == "websocket" && r.Header.Get("Connection") == "Upgrade"

		// For WebSocket requests, log basic info and pass through without wrapping
		if isWebSocket {
			log.Printf("[WEBSOCKET REQUEST] %s %s | IP: %s | User-Agent: %s",
				r.Method,
				r.URL.Path,
				getClientIP(r),
				r.UserAgent(),
			)
			next.ServeHTTP(w, r)
			duration := time.Since(start)
			log.Printf("[WEBSOCKET] %s %s | Duration: %v", r.Method, r.URL.Path, duration)
			return
		}

		// Read request body for non-WebSocket requests
		var requestBody []byte
		if r.Body != nil {
			requestBody, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		// Wrap response writer to capture response
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     200,
			body:           &bytes.Buffer{},
		}

		// Log incoming request
		log.Printf("[REQUEST] %s %s | IP: %s | User-Agent: %s | Content-Length: %d",
			r.Method,
			r.URL.Path,
			getClientIP(r),
			r.UserAgent(),
			len(requestBody),
		)

		// Log request body for POST/PUT requests (truncated for large bodies)
		if r.Method == "POST" || r.Method == "PUT" {
			bodyStr := string(requestBody)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500] + "... [truncated]"
			}
			log.Printf("[REQUEST BODY] %s %s | Body: %s", r.Method, r.URL.Path, bodyStr)
		}

		// Log query parameters
		if len(r.URL.RawQuery) > 0 {
			log.Printf("[REQUEST PARAMS] %s %s | Query: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Log response
		responseBodyStr := wrapped.body.String()
		if len(responseBodyStr) > 500 {
			responseBodyStr = responseBodyStr[:500] + "... [truncated]"
		}

		log.Printf("[RESPONSE] %s %s | Status: %d | Duration: %v | Size: %d bytes",
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			duration,
			wrapped.body.Len(),
		)

		// Log response body for errors or specific endpoints
		if wrapped.statusCode >= 400 || shouldLogResponseBody(r.URL.Path) {
			log.Printf("[RESPONSE BODY] %s %s | Status: %d | Body: %s",
				r.Method,
				r.URL.Path,
				wrapped.statusCode,
				responseBodyStr,
			)
		}

		// Log slow requests
		if duration > 5*time.Second {
			log.Printf("[SLOW REQUEST] %s %s | Duration: %v | Status: %d",
				r.Method,
				r.URL.Path,
				duration,
				wrapped.statusCode,
			)
		}
	})
}

// getClientIP extracts the real client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// shouldLogResponseBody determines if response body should be logged
func shouldLogResponseBody(path string) bool {
	// Log response bodies for specific endpoints
	logPaths := []string{
		"/health",
		"/jobs",
	}

	for _, logPath := range logPaths {
		if path == logPath {
			return true
		}
	}

	return false
}