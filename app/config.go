package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"strings"
	"time"

	"github.com/team-telnyx/telnyx-go/v3"
	"github.com/team-telnyx/telnyx-go/v3/option"
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
	UploadDir           string
	UploadInMemory      bool
	memStore            map[string][]byte
	memTypes            map[string]string
	AuthConfig          AuthConfig
}

// Config holds the configuration values for the application
type Config struct {
	APIKey         string
	DefaultFrom    string
	DefaultConn    string
	FaxAppID       string
	Hipaa          bool
	PublicBaseURL  string
	UploadDir      string
	UploadInMemory bool
	Port           string
	AuthConfig     AuthConfig
}

// LoadConfig loads configuration from environment variables and command-line flags
func LoadConfig() *Config {
	// Flags and env config
	apiKey := os.Getenv("TELNYX_API_KEY")
	defaultFromEnv := firstNonEmpty(os.Getenv("FAX_FROM_DEFAULT"), os.Getenv("FROM_NUMBER"))
	defaultConnEnv := firstNonEmpty(os.Getenv("FAX_CONNECTION_ID"), os.Getenv("TELNYX_CONNECTION_ID"))
	faxAppEnv := os.Getenv("FAX_APPLICATION_ID")
	hipaaEnv := firstNonEmpty(os.Getenv("HIPPA_MODE"), os.Getenv("HIPAA_MODE"))
	publicBaseURL := os.Getenv("PUBLIC_BASE_URL")

	// Auth config from environment
	authPassword := os.Getenv("AUTH_PASSWORD")
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" && authPassword != "" {
		// Generate a default session secret if password is set but secret isn't
		sessionSecret = "change-me-" + authPassword[:min(len(authPassword), 10)]
	}

	faxAppFlag := flag.String("fax_app_id", "", "Telnyx Fax Application ID for managing settings and auto-detecting connection ID")
	fromFlag := flag.String("from", "", "Default 'from' number (E.164) to prefill and use when form provides none.")
	connectionFlag := flag.String("connection_id", "", "Default Telnyx connection ID to use when the form provides none.")
	hipaaFlag := flag.Bool("hipaa", false, "Enable HIPAA mode: disables storing media/preview.")
	publicBaseURLFlag := flag.String("public_base_url", "", "Public base URL for serving uploaded files (e.g., https://yourdomain). If empty, uses http://localhost:PORT.")
	uploadDirFlag := flag.String("upload_dir", "uploads", "Directory to store uploaded files.")
	uploadInMemoryFlag := flag.Bool("upload_in_memory", false, "Store uploaded files in memory and serve from the app (no persistent volume).")
	flag.Parse()

	defaultFrom := firstNonEmpty(*fromFlag, defaultFromEnv)
	defaultConn := firstNonEmpty(*connectionFlag, defaultConnEnv)
	faxAppID := firstNonEmpty(*faxAppFlag, faxAppEnv)
	hipaa := *hipaaFlag || strings.EqualFold(hipaaEnv, "true") || hipaaEnv == "1"
	uploadDir := *uploadDirFlag
	uploadInMemory := *uploadInMemoryFlag || strings.EqualFold(os.Getenv("UPLOAD_IN_MEMORY"), "true") || os.Getenv("UPLOAD_IN_MEMORY") == "1"
	if *publicBaseURLFlag != "" {
		publicBaseURL = *publicBaseURLFlag
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{
		APIKey:         apiKey,
		DefaultFrom:    defaultFrom,
		DefaultConn:    defaultConn,
		FaxAppID:       faxAppID,
		Hipaa:          hipaa,
		PublicBaseURL:  publicBaseURL,
		UploadDir:      uploadDir,
		UploadInMemory: uploadInMemory,
		Port:           port,
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

	// Try to load templates from app/web/templates (when running from project root)
	// or ../web/templates (when running from app directory)
	tmpl, err := template.ParseGlob("app/web/templates/*.html")
	if err != nil {
		// Try when running from within app directory
		tmpl, err = template.ParseGlob("../web/templates/*.html")
		if err != nil {
			// Last try: web/templates (legacy support)
			tmpl, err = template.ParseGlob("web/templates/*.html")
			if err != nil {
				return nil, fmt.Errorf("failed to parse templates: %w", err)
			}
		}
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
		UploadInMemory:      cfg.UploadInMemory,
		memStore:            make(map[string][]byte),
		memTypes:            make(map[string]string),
		AuthConfig:          cfg.AuthConfig,
	}

	// Set BaseURL in auth config if not already set
	if app.AuthConfig.BaseURL == "" {
		app.AuthConfig.BaseURL = publicBaseURL
	}

	return app, nil
}
