package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cocina/server-mvp/config"
	"github.com/cocina/server-mvp/database"
	"github.com/cocina/server-mvp/handlers"
	"github.com/cocina/server-mvp/messaging"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize database
	db, err := database.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize WebSocket hub for real-time messaging
	wsHub := messaging.NewWebSocketHub()
	go wsHub.Run()

	// Create API handlers
	apiHandlers := handlers.NewAPIHandler(db.GetConn(), wsHub, cfg.ServerURL)

	// Setup routes
	mux := http.NewServeMux()

	// Authentication endpoints (REST over PocketBase auth)
	mux.HandleFunc("/api/v1/auth/register", apiHandlers.Register)
	mux.HandleFunc("/api/v1/auth/login", apiHandlers.Login)
	mux.HandleFunc("/api/v1/auth/refresh", apiHandlers.RefreshToken)
	mux.HandleFunc("/api/v1/auth/logout", apiHandlers.Logout)

	// User endpoints
	mux.HandleFunc("/api/v1/users/me", apiHandlers.GetMe)
	mux.HandleFunc("/api/v1/users/me/presence", apiHandlers.UpdatePresence)

	// Message endpoints (REST API)
	mux.HandleFunc("/api/v1/messages/send", apiHandlers.SendMessage)
	mux.HandleFunc("/api/v1/messages/history", apiHandlers.GetMessageHistory)
	
	// Conversations/DMs endpoints (legacy)
	mux.HandleFunc("/api/v1/conversations", apiHandlers.GetConversations)

	// Organization / workspace / channel API (v1 aligned)
	mux.HandleFunc("/api/v1/orgs", apiHandlers.DispatchAPI)
	mux.HandleFunc("/api/v1/orgs/", apiHandlers.DispatchAPI)
	mux.HandleFunc("/api/v1/workspaces/", apiHandlers.DispatchAPI)
	mux.HandleFunc("/api/v1/channels/", apiHandlers.DispatchAPI)

	// WebSocket endpoint for real-time messaging
	mux.HandleFunc("/ws", apiHandlers.HandleWebSocket)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Apply CORS middleware to all routes EXCEPT WebSocket
	// WebSocket handler needs special treatment - it upgrades the connection
	handler := http.Handler(mux)
	corsHandler := corsMiddleware(handler)

	// Create HTTP server
	addr := ":" + cfg.Port
	server := &http.Server{
		Addr:         addr,
		Handler:      corsHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Cocina Server MVP starting on http://localhost%s", addr)
	log.Printf("Database path: %s", cfg.DBPath)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}

// corsMiddleware adds CORS headers to all responses
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		// Allow specific origins or all for development
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			// For WebSocket upgrade attempts, we need to allow the necessary headers
			connection := r.Header.Get("Connection")
			upgrade := r.Header.Get("Upgrade")
			
			// If this looks like a WebSocket upgrade attempt (even in preflight)
			if connection == "Upgrade" || upgrade == "websocket" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Upgrade, Connection")
				// Don't return 204 - let the request continue to the handler
			} else {
				// Regular preflight request - respond with no content
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		// Add standard headers for non-preflight requests
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")

		next.ServeHTTP(w, r)
	})
}
