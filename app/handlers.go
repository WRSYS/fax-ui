package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/team-telnyx/telnyx-go/v4"
)

// handleHome renders the main fax sending form
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fromQS := r.URL.Query().Get("from")
	prefillFrom := firstNonEmpty(fromQS, a.DefaultFrom)
	connQS := r.URL.Query().Get("connection_id")
	prefillConn := firstNonEmpty(connQS, a.DefaultConnectionID)
	data := map[string]any{
		"HasAPIKey":           os.Getenv("TELNYX_API_KEY") != "",
		"PrefillFrom":         prefillFrom,
		"PrefillConnectionID": prefillConn,
		"ShowSettings":        a.FaxApplicationID != "",
		"Hipaa":               a.Hipaa,
		"HideFrom":            strings.TrimSpace(prefillFrom) != "",
		"HideConnectionID":    strings.TrimSpace(prefillConn) != "",
	}
	if err := a.Tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleFax routes POST requests to send a fax and GET requests to show fax details
func (a *App) handleFax(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.handleSendFax(w, r)
	case http.MethodGet:
		a.handleShowFax(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSendFax processes the fax send form and sends a fax via Telnyx API
func (a *App) handleSendFax(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(25 << 20); err != nil {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
	}

	connectionID := r.FormValue("connection_id")
	if connectionID == "" {
		connectionID = a.DefaultConnectionID
	}
	from := normalizePhoneNumber(r.FormValue("from"))
	if from == "" {
		from = a.DefaultFrom
	}
	to := normalizePhoneNumber(r.FormValue("to"))
	mediaURL := r.FormValue("media_url")
	webhookURL := r.FormValue("webhook_url")
	storePreview := r.FormValue("store_preview") == "on"
	storeMedia := r.FormValue("store_media") == "on"
	quality := r.FormValue("quality")

	if connectionID == "" || from == "" || to == "" {
		http.Error(w, "connection_id, from and to are required", http.StatusBadRequest)
		return
	}

	// Handle file upload if present
	uploadedURL, err := a.handleFileUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build fax parameters
	params := telnyx.FaxNewParams{
		ConnectionID: connectionID,
		From:         from,
		To:           to,
	}

	// Set HIPAA defaults
	if a.Hipaa {
		params.StorePreview = telnyx.Bool(false)
		params.StoreMedia = telnyx.Bool(false)
	}

	// Set media URL from upload or form field
	if uploadedURL != "" {
		params.MediaURL = telnyx.String(uploadedURL)
	} else if mediaURL != "" {
		params.MediaURL = telnyx.String(mediaURL)
	} else {
		http.Error(w, "media_url or media_file is required", http.StatusBadRequest)
		return
	}

	// Optional parameters
	if webhookURL != "" {
		params.WebhookURL = telnyx.String(webhookURL)
	}
	if storePreview && !a.Hipaa {
		params.StorePreview = telnyx.Bool(true)
	}
	if storeMedia && !a.Hipaa {
		params.StoreMedia = telnyx.Bool(true)
	}
	switch quality {
	case "normal", "high", "very_high", "ultra_light", "ultra_dark":
		params.Quality = telnyx.FaxNewParamsQuality(quality)
	}

	// Send the fax
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, err := a.Client.Faxes.New(ctx, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	data := map[string]any{
		"Fax": res.Data,
	}
	if err := a.Tmpl.ExecuteTemplate(w, "fax_show.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleShowFax retrieves and displays details for a specific fax by ID
func (a *App) handleShowFax(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := a.Client.Faxes.Get(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	data := map[string]any{
		"Fax": res.Data,
	}
	if err := a.Tmpl.ExecuteTemplate(w, "fax_show.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleFaxes lists all faxes with pagination support
func (a *App) handleFaxes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	size := int64(10)
	number := int64(1)
	if v := r.URL.Query().Get("page_size"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			size = n
		}
	}
	if v := r.URL.Query().Get("page_number"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			number = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := a.Client.Faxes.List(ctx, telnyx.FaxListParams{
		PageNumber: telnyx.Int(number),
		PageSize:   telnyx.Int(size),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	data := map[string]any{
		"Faxes":      res.Data,
		"PageSize":   size,
		"PageNumber": number,
	}
	if err := a.Tmpl.ExecuteTemplate(w, "faxes.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleMediaServe serves uploaded files for Telnyx to fetch.
// This endpoint is publicly accessible (no auth required) but uses unguessable tokens for security.
// In HIPAA mode: files are in-memory and automatically cleaned up after expiration.
// In non-HIPAA mode with persistence: files are served from disk.
func (a *App) handleMediaServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract token/filename from path
	token := strings.TrimPrefix(r.URL.Path, "/media/")
	token = strings.TrimSpace(token)
	if token == "" {
		http.NotFound(w, r)
		return
	}

	// Non-HIPAA mode with disk storage: serve from filesystem
	if !a.Hipaa && a.UploadDir != "" {
		filePath := filepath.Join(a.UploadDir, filepath.Clean(token))
		// Ensure the path is within UploadDir (prevent directory traversal)
		if !strings.HasPrefix(filePath, filepath.Clean(a.UploadDir)+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
		return
	}

	// HIPAA mode or no disk storage: serve from memory
	a.memMu.RLock()
	file, ok := a.uploadedFiles[token]
	a.memMu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	// Check if file has expired
	if time.Now().After(file.ExpiresAt) {
		// Clean up expired file
		a.memMu.Lock()
		delete(a.uploadedFiles, token)
		a.memMu.Unlock()
		http.NotFound(w, r)
		return
	}

	if file.Type != "" {
		w.Header().Set("Content-Type", file.Type)
	}
	http.ServeContent(w, r, token, time.Now(), bytesReader(file.Data))
}

// logRequests is a middleware that logs HTTP requests
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// bytesReader returns an io.ReadSeeker for a byte slice
func bytesReader(b []byte) io.ReadSeeker {
	return bytes.NewReader(b)
}
