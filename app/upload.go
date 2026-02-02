package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// uploadedFile represents a file stored in memory for Telnyx to fetch
type uploadedFile struct {
	Data      []byte
	Type      string
	ExpiresAt time.Time
}

// handleFileUpload processes file uploads from the multipart form
// Returns the URL where the uploaded file can be accessed, or empty string if no file was uploaded
func (a *App) handleFileUpload(r *http.Request) (string, error) {
	// Check if there's a multipart form with files
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return "", nil
	}

	files := r.MultipartForm.File["media_file"]
	if len(files) == 0 {
		return "", nil
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("failed to read uploaded file: %w", err)
	}
	defer file.Close()

	// HIPAA mode always uses in-memory storage with auto-cleanup
	// Non-HIPAA mode with UploadDir uses disk storage
	if a.Hipaa || a.UploadDir == "" {
		return a.storeFileInMemory(file, fileHeader)
	}
	return a.storeFileToDisk(file, fileHeader)
}

// storeFileInMemory stores the uploaded file in memory with an unguessable token
// Files are automatically cleaned up after expiration (HIPAA compliant)
func (a *App) storeFileInMemory(file multipart.File, fileHeader *multipart.FileHeader) (string, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		return "", fmt.Errorf("failed to buffer uploaded file: %w", err)
	}

	// Generate cryptographically secure unguessable token
	token, err := generateSecureToken(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}

	// Store content type
	ctype := fileHeader.Header.Get("Content-Type")
	if ctype == "" {
		ctype = "application/octet-stream"
	}

	// Store file with expiration (30 minutes should be plenty for Telnyx to fetch)
	a.memMu.Lock()
	a.uploadedFiles[token] = uploadedFile{
		Data:      buf.Bytes(),
		Type:      ctype,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	a.memMu.Unlock()

	// Return the public URL where Telnyx can fetch this file
	uploadedURL := fmt.Sprintf("%s/media/%s", trimTrailingSlash(a.PublicBaseURL), token)
	return uploadedURL, nil
}

// storeFileToDisk stores the uploaded file to disk with an unguessable token filename
// Used in non-HIPAA mode when persistence is enabled
func (a *App) storeFileToDisk(file multipart.File, fileHeader *multipart.FileHeader) (string, error) {
	// Ensure upload directory exists
	if err := os.MkdirAll(a.UploadDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare upload storage: %w", err)
	}

	// Generate cryptographically secure unguessable token
	token, err := generateSecureToken(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}

	// Determine file extension from content type or original filename
	ext := filepath.Ext(fileHeader.Filename)
	if ext == "" {
		ctype := fileHeader.Header.Get("Content-Type")
		switch ctype {
		case "application/pdf":
			ext = ".pdf"
		case "image/tiff":
			ext = ".tiff"
		}
	}

	// Create file with unguessable name
	filename := token + ext
	destPath := filepath.Join(a.UploadDir, filename)

	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to store uploaded file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		return "", fmt.Errorf("failed to save uploaded file: %w", err)
	}

	// Return the public URL where Telnyx can fetch this file
	uploadedURL := fmt.Sprintf("%s/media/%s", trimTrailingSlash(a.PublicBaseURL), filename)
	return uploadedURL, nil
}

// generateSecureToken generates a cryptographically secure random token
func generateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// startFileCleanup starts a background goroutine that periodically removes expired files
func (a *App) startFileCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			a.cleanupExpiredFiles()
		}
	}()
}

// cleanupExpiredFiles removes files that have passed their expiration time
func (a *App) cleanupExpiredFiles() {
	now := time.Now()
	a.memMu.Lock()
	defer a.memMu.Unlock()

	for token, file := range a.uploadedFiles {
		if now.After(file.ExpiresAt) {
			delete(a.uploadedFiles, token)
			log.Printf("Cleaned up expired file: %s", token[:8]+"...")
		}
	}
}

// trimTrailingSlash removes trailing slashes from a URL string
func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
