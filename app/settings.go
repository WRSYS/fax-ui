package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/team-telnyx/telnyx-go/v4"
)

// handleSettings displays and updates fax application settings
func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	// Only show settings if a fax application ID is configured
	if a.FaxApplicationID == "" {
		http.Error(w, "Settings are only available when a fax application ID is configured. Use --fax_app_id or FAX_APPLICATION_ID environment variable.", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.handleShowSettings(w, r)
	case http.MethodPost:
		a.handleUpdateSettings(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleShowSettings fetches and displays current fax application settings
func (a *App) handleShowSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Fetch fax application details by fax application ID
	res, err := a.Client.FaxApplications.Get(ctx, a.FaxApplicationID)
	if err != nil {
		http.Error(w, "Failed to fetch fax application settings: "+err.Error(), http.StatusBadGateway)
		return
	}

	data := map[string]any{
		"Application":  res.Data,
		"FaxAppID":     a.FaxApplicationID,
		"ConnectionID": a.DefaultConnectionID,
		"Success":      r.URL.Query().Get("success") == "true",
		"Error":        r.URL.Query().Get("error"),
	}

	if err := a.Tmpl.ExecuteTemplate(w, "settings.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleUpdateSettings processes form submission to update fax application settings
func (a *App) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// First, fetch the current settings to get all required fields
	current, err := a.Client.FaxApplications.Get(ctx, a.FaxApplicationID)
	if err != nil {
		http.Error(w, "Failed to fetch current settings: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Build update parameters with form values
	params := telnyx.FaxApplicationUpdateParams{
		ApplicationName: current.Data.ApplicationName,
		WebhookEventURL: current.Data.WebhookEventURL,
	}

	// Update email recipient if provided
	emailRecipient := strings.TrimSpace(r.FormValue("fax_email_recipient"))
	if emailRecipient != "" {
		params.FaxEmailRecipient = telnyx.String(emailRecipient)
	}

	// Update webhook URL if provided
	webhookURL := strings.TrimSpace(r.FormValue("webhook_event_url"))
	if webhookURL != "" {
		params.WebhookEventURL = webhookURL
	}

	// Update failover webhook URL if provided
	failoverURL := strings.TrimSpace(r.FormValue("webhook_event_failover_url"))
	if failoverURL != "" {
		params.WebhookEventFailoverURL = telnyx.String(failoverURL)
	}

	// Update webhook timeout if provided
	webhookTimeout := r.FormValue("webhook_timeout_secs")
	if webhookTimeout != "" {
		if timeout, err := strconv.ParseInt(webhookTimeout, 10, 64); err == nil && timeout > 0 {
			params.WebhookTimeoutSecs = telnyx.Int(timeout)
		}
	}

	// Update inbound settings
	inbound := telnyx.FaxApplicationUpdateParamsInbound{}
	hasInboundUpdates := false

	channelLimit := r.FormValue("channel_limit")
	if channelLimit != "" {
		if limit, err := strconv.ParseInt(channelLimit, 10, 64); err == nil {
			inbound.ChannelLimit = telnyx.Int(limit)
			hasInboundUpdates = true
		}
	}

	sipSubdomain := strings.TrimSpace(r.FormValue("sip_subdomain"))
	if sipSubdomain != "" {
		inbound.SipSubdomain = telnyx.String(sipSubdomain)
		hasInboundUpdates = true
	}

	sipReceiveSettings := r.FormValue("sip_subdomain_receive_settings")
	if sipReceiveSettings != "" {
		inbound.SipSubdomainReceiveSettings = sipReceiveSettings
		hasInboundUpdates = true
	}

	if hasInboundUpdates {
		params.Inbound = inbound
	}

	// Update the fax application
	_, err = a.Client.FaxApplications.Update(ctx, a.FaxApplicationID, params)
	if err != nil {
		http.Redirect(w, r, "/settings?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings?success=true", http.StatusSeeOther)
}
