package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

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

	if a.UploadInMemory {
		return a.saveFileInMemory(file, fileHeader)
	}
	return a.saveFileToDisk(file, fileHeader)
}

// saveFileInMemory stores the uploaded file in memory
func (a *App) saveFileInMemory(file multipart.File, fileHeader *multipart.FileHeader) (string, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		return "", fmt.Errorf("failed to buffer uploaded file: %w", err)
	}

	// Generate unique ID for the file
	id := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(fileHeader.Filename))
	a.memStore[id] = buf.Bytes()

	// Store content type
	ctype := fileHeader.Header.Get("Content-Type")
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	a.memTypes[id] = ctype

	// Return the URL where this file can be accessed
	uploadedURL := fmt.Sprintf("%s/mem-uploads/%s", trimTrailingSlash(a.PublicBaseURL), id)
	return uploadedURL, nil
}

// saveFileToDisk stores the uploaded file to the filesystem
func (a *App) saveFileToDisk(file multipart.File, fileHeader *multipart.FileHeader) (string, error) {
	// Ensure upload directory exists
	if err := os.MkdirAll(a.UploadDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare upload storage: %w", err)
	}

	// Generate safe filename with timestamp
	safeName := sanitizeFilename(fileHeader.Filename)
	ts := time.Now().Unix()
	dest := filepath.Join(a.UploadDir, fmt.Sprintf("%d_%s", ts, safeName))

	// Create the destination file
	out, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("failed to store uploaded file: %w", err)
	}
	defer out.Close()

	// Copy file contents
	if _, err := io.Copy(out, file); err != nil {
		return "", fmt.Errorf("failed to save uploaded file: %w", err)
	}

	// Return the URL where this file can be accessed
	uploadedURL := fmt.Sprintf("%s/uploads/%s", trimTrailingSlash(a.PublicBaseURL), filepath.Base(dest))
	return uploadedURL, nil
}

// trimTrailingSlash removes trailing slashes from a URL string
func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
