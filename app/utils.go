package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// firstNonEmpty returns the first non-empty string from the provided values
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// normalizePhoneNumber converts a phone number to E.164 format
// Assumes US/Canada (country code 1) if no country code is provided
func normalizePhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}

	// If it looks like a SIP URI, return as-is
	if strings.HasPrefix(strings.ToLower(phone), "sip:") {
		return phone
	}

	// Remove all non-digit characters except leading +
	hasPlus := strings.HasPrefix(phone, "+")
	digits := regexp.MustCompile(`\D`).ReplaceAllString(phone, "")

	// If already has + and digits, validate and return
	if hasPlus {
		if len(digits) >= 10 {
			return "+" + digits
		}
		// Invalid format, continue to normalize
	}

	// Handle different digit lengths
	switch len(digits) {
	case 10:
		// US/Canada number without country code: 5551234567 -> +15551234567
		return "+1" + digits
	case 11:
		// Likely has country code 1: 15551234567 -> +15551234567
		if strings.HasPrefix(digits, "1") {
			return "+" + digits
		}
		// Unknown 11-digit format, assume it needs +1
		return "+1" + digits
	case 12, 13, 14, 15:
		// International number, just add +
		return "+" + digits
	default:
		// Less than 10 digits or very long, return with + if digits exist
		if len(digits) > 0 {
			return "+" + digits
		}
		return phone
	}
}

// sanitizeFilename removes potentially dangerous characters from filenames
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "..", ".")
	name = strings.ReplaceAll(name, " ", "_")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "file"
	}
	return b.String()
}

// detectNgrokPublicURL queries the ngrok API to find the public URL of the tunnel
func detectNgrokPublicURL(apiBase string) string {
	type tunnel struct {
		PublicURL string `json:"public_url"`
	}
	var resp struct {
		Tunnels []tunnel `json:"tunnels"`
	}
	url := strings.TrimRight(apiBase, "/") + "/api/tunnels"
	httpClient := &http.Client{Timeout: 3 * time.Second}
	res, err := httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ""
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return ""
	}
	var httpURL, httpsURL string
	for _, t := range resp.Tunnels {
		if strings.HasPrefix(t.PublicURL, "https://") {
			httpsURL = t.PublicURL
			break
		}
		if strings.HasPrefix(t.PublicURL, "http://") {
			httpURL = t.PublicURL
		}
	}
	if httpsURL != "" {
		return httpsURL
	}
	return httpURL
}
