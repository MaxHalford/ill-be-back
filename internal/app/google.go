package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func isGoogle(provider string) bool { return provider == "gmail" || provider == "calendar" }
func googleScope(provider string) string {
	if provider == "gmail" {
		return "https://www.googleapis.com/auth/gmail.settings.basic"
	}
	return "https://www.googleapis.com/auth/calendar.events.owned"
}

// Google errors can contain mailbox/event data. Expose only controlled messages.
func (a *Server) googleRequest(ctx context.Context, method, endpoint, token string, body io.Reader, form bool, out any) error {
	r, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	r.Header.Set("Accept", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if form {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := a.cfg.HTTPClient.Do(r)
	if err != nil {
		return &providerError{message: "Google hasn’t confirmed the update. Please retry."}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return &providerError{message: "Google sent an incomplete response. Please retry."}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := &providerError{status: response.StatusCode, message: "Google couldn’t complete this request. Please retry."}
		var oauth struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &oauth)
		if response.StatusCode == 401 || oauth.Error == "invalid_grant" {
			failure.message = "Your Google connection expired or was revoked. Please reconnect this account."
			failure.reconnect = true
		} else if response.StatusCode == 403 {
			failure.message = "Google didn’t allow this update. Check your account permissions and reconnect."
			failure.reconnect = true
		} else if response.StatusCode == 429 {
			failure.message = "Google is asking us to slow down. Please try again in a minute."
		} else if response.StatusCode == 400 && strings.Contains(endpoint, "/calendar/") {
			failure.message = "Google Calendar couldn’t create this out-of-office event. Check that your account supports out-of-office events."
		}
		return failure
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return &providerError{message: "Google sent an unexpected response. Please retry."}
	}
	return nil
}

type googleToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Scope   string `json:"scope"`
	Type    string `json:"token_type"`
}

func (a *Server) exchangeGoogle(ctx context.Context, provider, code, verifier string) (identity, error) {
	form := url.Values{"client_id": {a.cfg.GoogleClientID}, "client_secret": {a.cfg.GoogleClientSecret}, "code": {code}, "code_verifier": {verifier}, "grant_type": {"authorization_code"}, "redirect_uri": {a.cfg.AppURL + "/auth/" + provider + "/callback"}}
	var access googleToken
	if err := a.googleRequest(ctx, "POST", "https://oauth2.googleapis.com/token", "", strings.NewReader(form.Encode()), true, &access); err != nil {
		return identity{}, err
	}
	if access.Access == "" || access.Refresh == "" || !strings.EqualFold(access.Type, "Bearer") || !hasScope(access.Scope, googleScope(provider)) {
		return identity{}, errors.New("Google did not grant the required offline permission")
	}
	var user struct {
		Sub      string `json:"sub"`
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := a.googleRequest(ctx, "GET", "https://openidconnect.googleapis.com/v1/userinfo", access.Access, nil, false, &user); err != nil {
		return identity{}, err
	}
	if user.Sub == "" || user.Email == "" || !user.Verified {
		return identity{}, errors.New("Google did not confirm the account identity")
	}
	// Persist only the refresh credential; short-lived access tokens stay in memory.
	return identity{user.Sub, user.Email, user.Email, access.Refresh}, nil
}
func (a *Server) googleAccessToken(ctx context.Context, c Connection) (string, error) {
	refresh, err := a.store.decrypt(c.token, c.Provider+":"+c.remoteID)
	if err != nil {
		return "", &providerError{message: "Please reconnect this Google account.", reconnect: true}
	}
	form := url.Values{"client_id": {a.cfg.GoogleClientID}, "client_secret": {a.cfg.GoogleClientSecret}, "refresh_token": {refresh}, "grant_type": {"refresh_token"}}
	var access googleToken
	if err = a.googleRequest(ctx, "POST", "https://oauth2.googleapis.com/token", "", strings.NewReader(form.Encode()), true, &access); err != nil {
		return "", err
	}
	if access.Access == "" || !strings.EqualFold(access.Type, "Bearer") {
		return "", &providerError{message: "Google didn’t return an access token. Please reconnect.", reconnect: true}
	}
	if access.Scope != "" && !hasScope(access.Scope, googleScope(c.Provider)) {
		return "", &providerError{message: "Google access no longer includes this app. Please reconnect.", reconnect: true}
	}
	if access.Refresh != "" && access.Refresh != refresh {
		encrypted := a.store.encrypt(access.Refresh, c.Provider+":"+c.remoteID)
		if _, err = a.store.db.ExecContext(ctx, `UPDATE connections SET token=$1 WHERE id=$2`, encrypted, c.ID); err != nil {
			return "", err
		}
	}
	return access.Access, nil
}

type vacationSettings struct {
	Enabled  bool   `json:"enableAutoReply"`
	Subject  string `json:"responseSubject"`
	Body     string `json:"responseBodyPlainText"`
	HTML     string `json:"responseBodyHtml"`
	Contacts bool   `json:"restrictToContacts"`
	Domain   bool   `json:"restrictToDomain"`
	Start    string `json:"startTime,omitempty"`
	End      string `json:"endTime,omitempty"`
}

func (a *Server) setGmailStatus(ctx context.Context, c Connection, target string) error {
	token, err := a.googleAccessToken(ctx, c)
	if err != nil {
		return err
	}
	endpoint := "https://gmail.googleapis.com/gmail/v1/users/me/settings/vacation"
	var settings vacationSettings
	if err = a.googleRequest(ctx, "GET", endpoint, token, nil, false, &settings); err != nil {
		return err
	}
	settings.Enabled = target == "away"
	if settings.Enabled {
		settings.Subject = "Out of office"
		settings.Body = c.Message
		settings.HTML = ""
		settings.Start = strconv.FormatInt(time.Now().UnixMilli(), 10)
		settings.End = ""
	}
	var confirmed vacationSettings
	if err = a.googleRequest(ctx, "PUT", endpoint, token, payload(settings), false, &confirmed); err != nil {
		return err
	}
	if confirmed.Enabled != settings.Enabled || (settings.Enabled && (confirmed.Body != settings.Body || confirmed.HTML != "" || confirmed.End != "")) || confirmed.Contacts != settings.Contacts || confirmed.Domain != settings.Domain {
		return &providerError{message: "Gmail hasn’t confirmed the requested vacation settings. Please retry."}
	}
	return nil
}
