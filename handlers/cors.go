package handlers

import (
	"log"
	"net/http"
)

// CORSMiddleware adds Cross-Origin Resource Sharing headers to HTTP responses
type CORSMiddleware struct {
	allowedOrigins []string
}

// NewCORSMiddleware creates a CORS middleware with default development origins
func NewCORSMiddleware() *CORSMiddleware {
	return &CORSMiddleware{
		allowedOrigins: []string{
			"http://localhost:5173", // Vite dev server
			"http://localhost:3000", // Alternative port
			"http://127.0.0.1:5173",
			"http://127.0.0.1:3000",
		},
	}
}

// AllowOrigin checks if the origin is allowed
func (m *CORSMiddleware) AllowOrigin(origin string) bool {
	for _, allowed := range m.allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// Middleware adds CORS headers to all responses
func (m *CORSMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		// Check if origin is allowed
		if m.AllowOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			// For development, allow all (NOT recommended for production)
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Add standard headers for non-preflight requests
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")

		next.ServeHTTP(w, r)
	})
}

// LogCORS wraps the CORS middleware with logging for debugging
func LogCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[CORS] %s %s from %s", r.Method, r.URL.Path, r.Header.Get("Origin"))
		
		// Add CORS headers for debugging
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
