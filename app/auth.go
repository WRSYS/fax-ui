package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/microsoft"
)

const (
	sessionCookieName = "fax_ui_session"
	sessionMaxAge     = 24 * time.Hour
)

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Password           string
	SessionSecret      string
	GoogleClientID     string
	GoogleClientSecret string
	MicrosoftClientID  string
	MicrosoftSecret    string
	GitHubClientID     string
	GitHubSecret       string
	BaseURL            string
}

// generateSessionToken creates a secure random session token
func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// signSessionToken creates an HMAC signature for the session token
func signSessionToken(token, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(token))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

// verifySessionToken verifies the HMAC signature of a session token
func verifySessionToken(token, signature, secret string) bool {
	expected := signSessionToken(token, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// setSessionCookie sets an authenticated session cookie
func (a *App) setSessionCookie(w http.ResponseWriter, userInfo string) error {
	token, err := generateSessionToken()
	if err != nil {
		return err
	}

	signature := signSessionToken(token, a.AuthConfig.SessionSecret)
	value := fmt.Sprintf("%s.%s.%s", token, signature, userInfo)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.PublicBaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// clearSessionCookie removes the session cookie
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// hasAuthConfigured returns true if any authentication method is configured
func (a *App) hasAuthConfigured() bool {
	return a.AuthConfig.Password != "" ||
		a.AuthConfig.GoogleClientID != "" ||
		a.AuthConfig.MicrosoftClientID != "" ||
		a.AuthConfig.GitHubClientID != ""
}

// isAuthenticated checks if the request has a valid session
func (a *App) isAuthenticated(r *http.Request) bool {
	// If no auth is configured, allow access (open mode)
	if !a.hasAuthConfigured() {
		return true
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}

	parts := strings.SplitN(cookie.Value, ".", 3)
	if len(parts) != 3 {
		return false
	}

	token, signature := parts[0], parts[1]
	return verifySessionToken(token, signature, a.AuthConfig.SessionSecret)
}

// requireAuth is middleware that requires authentication
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAuthenticated(r) {
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// handleLogin shows the login page or processes login
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	// If no auth configured, redirect to home
	if !a.hasAuthConfigured() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Already authenticated
	if a.isAuthenticated(r) {
		redirect := r.URL.Query().Get("redirect")
		if redirect == "" {
			redirect = "/"
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		a.handlePasswordLogin(w, r)
		return
	}

	// Show login page
	data := map[string]any{
		"Error":        r.URL.Query().Get("error"),
		"Redirect":     r.URL.Query().Get("redirect"),
		"HasGoogle":    a.AuthConfig.GoogleClientID != "",
		"HasMicrosoft": a.AuthConfig.MicrosoftClientID != "",
		"HasGitHub":    a.AuthConfig.GitHubClientID != "",
		"HasPassword":  a.AuthConfig.Password != "",
	}

	if err := a.Tmpl.ExecuteTemplate(w, "login.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handlePasswordLogin processes password authentication
func (a *App) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	redirect := r.FormValue("redirect")
	// Validate redirect to prevent open redirect attacks
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = "/"
	}

	if password == a.AuthConfig.Password {
		if err := a.setSessionCookie(w, "password"); err != nil {
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/login?error=invalid&redirect="+redirect, http.StatusSeeOther)
}

// handleLogout clears the session and redirects to login
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// getOAuthConfig returns OAuth2 config for the specified provider
func (a *App) getOAuthConfig(provider string) *oauth2.Config {
	redirectURL := a.PublicBaseURL + "/auth/callback/" + provider

	switch provider {
	case "google":
		if a.AuthConfig.GoogleClientID == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     a.AuthConfig.GoogleClientID,
			ClientSecret: a.AuthConfig.GoogleClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
			Endpoint:     google.Endpoint,
		}
	case "microsoft":
		if a.AuthConfig.MicrosoftClientID == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     a.AuthConfig.MicrosoftClientID,
			ClientSecret: a.AuthConfig.MicrosoftSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"User.Read"},
			Endpoint:     microsoft.AzureADEndpoint("common"),
		}
	case "github":
		if a.AuthConfig.GitHubClientID == "" {
			return nil
		}
		return &oauth2.Config{
			ClientID:     a.AuthConfig.GitHubClientID,
			ClientSecret: a.AuthConfig.GitHubSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"user:email"},
			Endpoint:     github.Endpoint,
		}
	default:
		return nil
	}
}

// handleOAuthLogin redirects to OAuth provider
func (a *App) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimPrefix(r.URL.Path, "/auth/login/")
	config := a.getOAuthConfig(provider)
	if config == nil {
		http.Error(w, "OAuth provider not configured", http.StatusBadRequest)
		return
	}

	// Store redirect in cookie
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_redirect",
		Value:    redirect,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
	})

	state, _ := generateSessionToken()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
	})

	url := config.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleOAuthCallback processes OAuth callback
func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimPrefix(r.URL.Path, "/auth/callback/")

	// Verify state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	config := a.getOAuthConfig(provider)
	if config == nil {
		http.Error(w, "OAuth provider not configured", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	token, err := config.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}

	if !token.Valid() {
		http.Error(w, "invalid token", http.StatusInternalServerError)
		return
	}

	// Set session
	if err := a.setSessionCookie(w, provider); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	// Get redirect and validate to prevent open redirect attacks
	redirect := "/"
	if cookie, err := r.Cookie("oauth_redirect"); err == nil {
		if strings.HasPrefix(cookie.Value, "/") && !strings.HasPrefix(cookie.Value, "//") {
			redirect = cookie.Value
		}
	}

	// Clear OAuth state cookies (but NOT the session cookie!)
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "oauth_redirect", MaxAge: -1, Path: "/"})

	http.Redirect(w, r, redirect, http.StatusSeeOther)
}
