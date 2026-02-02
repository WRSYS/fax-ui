package main

import (
	"fmt"
	"log"
	"net/http"
)

// Version is the application version. Injected at build via -ldflags.
var Version = "dev"

func main() {
	// Load configuration from environment and flags
	cfg := LoadConfig()

	// Initialize the application
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatalf("failed to initialize app: %v", err)
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Auth routes (no auth required)
	mux.HandleFunc("/login", app.handleLogin)
	mux.HandleFunc("/logout", app.handleLogout)
	mux.HandleFunc("/auth/login/", app.handleOAuthLogin)
	mux.HandleFunc("/auth/callback/", app.handleOAuthCallback)

	// Public route for media files - Telnyx fetches from here during fax send
	// Secured by unguessable tokens in the URL, not by authentication
	mux.HandleFunc("/media/", app.handleMediaServe)

	// Protected routes
	mux.HandleFunc("/", app.requireAuth(app.handleHome))
	mux.HandleFunc("/fax", app.requireAuth(app.handleFax))
	mux.HandleFunc("/faxes", app.requireAuth(app.handleFaxes))
	mux.HandleFunc("/settings", app.requireAuth(app.handleSettings))

	// Create server with logging middleware
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Port),
		Handler: logRequests(mux),
	}

	log.Printf("fax-ui v%s listening on http://localhost:%s (public: %s)", Version, cfg.Port, app.PublicBaseURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
