package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/team-telnyx/telnyx-go/v4"
	"github.com/team-telnyx/telnyx-go/v4/option"
)

// App holds the application state and dependencies
type App struct {
	Client              *telnyx.Client
	Tmpl                *template.Template
	DefaultFrom         string
	DefaultConnectionID string
	FaxApplicationID    string
	Hipaa               bool
	PublicBaseURL       string
	UploadDir           string                  // directory for disk-based uploads (non-HIPAA mode)
	uploadedFiles       map[string]uploadedFile // token -> uploaded file for Telnyx to fetch
	memMu               sync.RWMutex            // protects uploadedFiles
	AuthConfig          AuthConfig
}

// Config holds the configuration values for the application
type Config struct {
	APIKey        string
	DefaultFrom   string
	DefaultConn   string
	FaxAppID      string
	Hipaa         bool
	PublicBaseURL string
	UploadDir     string
	Port          string
	AuthConfig    AuthConfig
}

// LoadConfig loads configuration from environment variables and command-line flags
func LoadConfig() *Config {
	// Flags and env config
	apiKey := os.Getenv("TELNYX_API_KEY")
	defaultFromEnv := firstNonEmpty(os.Getenv("FAX_FROM_DEFAULT"), os.Getenv("FROM_NUMBER"))
	defaultConnEnv := firstNonEmpty(os.Getenv("FAX_CONNECTION_ID"), os.Getenv("TELNYX_CONNECTION_ID"))
	faxAppEnv := os.Getenv("FAX_APPLICATION_ID")
	// Check for HIPAA mode (support both spellings, warn on typo)
	hipaaEnv := os.Getenv("HIPAA_MODE")
	if hipaaEnv == "" && os.Getenv("HIPPA_MODE") != "" {
		log.Println("Warning: HIPPA_MODE is deprecated, use HIPAA_MODE instead")
		hipaaEnv = os.Getenv("HIPPA_MODE")
	}
	publicBaseURL := os.Getenv("PUBLIC_BASE_URL")

	// Auth config from environment
	authPassword := os.Getenv("AUTH_PASSWORD")
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" && authPassword != "" {
		// Generate a default session secret if password is set but secret isn't
		log.Println("Warning: SESSION_SECRET not set, using auto-generated value. Set SESSION_SECRET for production.")
		sessionSecret = "change-me-" + authPassword[:min(len(authPassword), 10)]
	}

	faxAppFlag := flag.String("fax_app_id", "", "Telnyx Fax Application ID for managing settings and auto-detecting connection ID")
	fromFlag := flag.String("from", "", "Default 'from' number (E.164) to prefill and use when form provides none.")
	connectionFlag := flag.String("connection_id", "", "Default Telnyx connection ID to use when the form provides none.")
	hipaaFlag := flag.Bool("hipaa", false, "Enable HIPAA mode: in-memory only storage with auto-cleanup.")
	publicBaseURLFlag := flag.String("public_base_url", "", "Public base URL (e.g., https://yourdomain). Required for file uploads.")
	uploadDirFlag := flag.String("upload_dir", "", "Directory for persistent uploads (non-HIPAA mode). If empty, uses in-memory storage.")
	flag.Parse()

	defaultFrom := firstNonEmpty(*fromFlag, defaultFromEnv)
	defaultConn := firstNonEmpty(*connectionFlag, defaultConnEnv)
	faxAppID := firstNonEmpty(*faxAppFlag, faxAppEnv)
	hipaa := *hipaaFlag || strings.EqualFold(hipaaEnv, "true") || hipaaEnv == "1"
	uploadDir := firstNonEmpty(*uploadDirFlag, os.Getenv("UPLOAD_DIR"))
	if *publicBaseURLFlag != "" {
		publicBaseURL = *publicBaseURLFlag
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{
		APIKey:        apiKey,
		DefaultFrom:   defaultFrom,
		DefaultConn:   defaultConn,
		FaxAppID:      faxAppID,
		Hipaa:         hipaa,
		PublicBaseURL: publicBaseURL,
		UploadDir:     uploadDir,
		Port:          port,
		AuthConfig: AuthConfig{
			Password:           authPassword,
			SessionSecret:      sessionSecret,
			GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			MicrosoftClientID:  os.Getenv("MICROSOFT_CLIENT_ID"),
			MicrosoftSecret:    os.Getenv("MICROSOFT_CLIENT_SECRET"),
			GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
			GitHubSecret:       os.Getenv("GITHUB_CLIENT_SECRET"),
		},
	}
}

// NewApp creates and initializes a new App instance with the given configuration
func NewApp(cfg *Config) (*App, error) {
	client := telnyx.NewClient(
		option.WithAPIKey(cfg.APIKey),
	)

	// Try to load templates from various possible locations
	// Priority: 1) web/templates (Docker/production), 2) app/web/templates (from project root), 3) ../web/templates (from app dir)
	templatePaths := []string{
		"web/templates/*.html",     // Docker: /app/web/templates
		"app/web/templates/*.html", // Running from project root
		"../web/templates/*.html",  // Running from within app directory
	}

	var tmpl *template.Template
	var err error
	for _, path := range templatePaths {
		tmpl, err = template.ParseGlob(path)
		if err == nil {
			log.Printf("Loaded templates from: %s", path)
			break
		}
	}
	if tmpl == nil {
		return nil, fmt.Errorf("failed to parse templates from any location: %w", err)
	}

	publicBaseURL := cfg.PublicBaseURL
	if publicBaseURL == "" {
		publicBaseURL = fmt.Sprintf("http://localhost:%s", cfg.Port)
	}

	// Check for ngrok and update public URL if available
	if api := os.Getenv("NGROK_API_URL"); strings.TrimSpace(api) != "" {
		if pub := detectNgrokPublicURL(api); pub != "" {
			publicBaseURL = pub
		}
	}

	// If fax application ID is provided, fetch it to get the connection ID
	defaultConn := cfg.DefaultConn
	if cfg.FaxAppID != "" && defaultConn == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		faxApp, err := client.FaxApplications.Get(ctx, cfg.FaxAppID)
		if err == nil && faxApp.Data.ID != "" {
			// Use the fax application ID as the connection ID
			defaultConn = faxApp.Data.ID
			log.Printf("Using fax application ID as connection ID: %s", defaultConn)
		} else if err != nil {
			log.Printf("Warning: Could not fetch fax application details: %v", err)
		}
	}

	app := &App{
		Client:              &client,
		Tmpl:                tmpl,
		DefaultFrom:         cfg.DefaultFrom,
		DefaultConnectionID: defaultConn,
		FaxApplicationID:    cfg.FaxAppID,
		Hipaa:               cfg.Hipaa,
		PublicBaseURL:       publicBaseURL,
		UploadDir:           cfg.UploadDir,
		uploadedFiles:       make(map[string]uploadedFile),
		AuthConfig:          cfg.AuthConfig,
	}

	// Start background cleanup of expired files (every 5 minutes) - only needed for in-memory mode
	if cfg.Hipaa || cfg.UploadDir == "" {
		app.startFileCleanup(5 * time.Minute)
	}

	// Set BaseURL in auth config if not already set
	if app.AuthConfig.BaseURL == "" {
		app.AuthConfig.BaseURL = publicBaseURL
	}

	return app, nil
}
