package app

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func isMicrosoft(p string) bool { return p == "teams" || p == "outlook" }

func microsoftScope(p string) string {
	if p == "teams" {
		return "Presence.ReadWrite"
	}
	return "MailboxSettings.ReadWrite"
}

func microsoftScopes(p string) string {
	return "openid profile email offline_access https://graph.microsoft.com/User.Read https://graph.microsoft.com/" + microsoftScope(p)
}

func microsoftAuthority(p string) string {
	tenant := "common" // Outlook also supports personal Microsoft accounts.
	if p == "teams" {
		tenant = "organizations"
	}
	return "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/"
}

// Do not expose Graph error bodies: they may include account or mailbox data.
func (a *Server) microsoftRequest(ctx context.Context, method, endpoint, token string, body io.Reader, form bool, out any) error {
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
		return &providerError{message: "Microsoft hasn’t confirmed the update. Please retry."}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return &providerError{message: "Microsoft sent an incomplete response. Please retry."}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := &providerError{status: response.StatusCode, message: "Microsoft couldn’t complete this request. Please retry."}
		var oauth struct {
			Error string `json:"error"`
		}
		var graph struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &oauth)
		_ = json.Unmarshal(data, &graph)
		switch {
		case response.StatusCode == 401 || oauth.Error == "invalid_grant" || oauth.Error == "interaction_required" || oauth.Error == "consent_required":
			failure.message = "Your Microsoft connection expired or needs approval. Please reconnect this account."
			failure.reconnect = true
		case graph.Error.Code == "MailboxNotEnabledForRESTAPI" || graph.Error.Code == "MailboxNotSupportedForRESTAPI":
			failure.message = "This account needs an Outlook.com or supported Microsoft 365 mailbox to use automatic replies."
		case response.StatusCode == 403:
			failure.message = "Microsoft didn’t allow this update. Your organization may require an administrator’s approval before you reconnect."
			failure.reconnect = true
		case response.StatusCode == 429:
			failure.message = "Microsoft is asking us to slow down. Please try again in a minute."
		}
		return failure
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return &providerError{message: "Microsoft sent an unexpected response. Please retry."}
	}
	return nil
}

type microsoftToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Scope   string `json:"scope"`
	Type    string `json:"token_type"`
}

type microsoftCredential struct {
	Refresh string `json:"refresh"`
	UserID  string `json:"userID"`
}

func hasMicrosoftScope(scopes, required string) bool {
	for _, scope := range strings.Fields(scopes) {
		if strings.EqualFold(strings.TrimPrefix(scope, "https://graph.microsoft.com/"), required) {
			return true
		}
	}
	return false
}

func (a *Server) exchangeMicrosoft(ctx context.Context, provider, code, verifier string) (identity, error) {
	form := url.Values{"client_id": {a.cfg.MicrosoftClientID}, "client_secret": {a.cfg.MicrosoftClientSecret}, "code": {code}, "code_verifier": {verifier}, "grant_type": {"authorization_code"}, "redirect_uri": {a.cfg.AppURL + "/auth/" + provider + "/callback"}, "scope": {microsoftScopes(provider)}}
	var access microsoftToken
	if err := a.microsoftRequest(ctx, "POST", microsoftAuthority(provider)+"token", "", strings.NewReader(form.Encode()), true, &access); err != nil {
		return identity{}, err
	}
	if access.Access == "" || access.Refresh == "" || !strings.EqualFold(access.Type, "Bearer") || !hasMicrosoftScope(access.Scope, microsoftScope(provider)) || !hasMicrosoftScope(access.Scope, "User.Read") {
		return identity{}, errors.New("Microsoft did not grant the required offline permission")
	}
	var subject struct {
		Sub string `json:"sub"`
	}
	if err := a.microsoftRequest(ctx, "GET", "https://graph.microsoft.com/oidc/userinfo", access.Access, nil, false, &subject); err != nil {
		return identity{}, err
	}
	var user struct {
		ID        string `json:"id"`
		Name      string `json:"displayName"`
		Mail      string `json:"mail"`
		Principal string `json:"userPrincipalName"`
	}
	if err := a.microsoftRequest(ctx, "GET", "https://graph.microsoft.com/v1.0/me?$select=id,displayName,mail,userPrincipalName", access.Access, nil, false, &user); err != nil {
		return identity{}, err
	}
	if subject.Sub == "" || user.ID == "" {
		return identity{}, errors.New("Microsoft did not confirm the account identity")
	}
	label := user.Mail
	if label == "" {
		label = user.Principal
	}
	if label == "" {
		label = user.Name
	}
	if label == "" {
		return identity{}, errors.New("Microsoft did not provide an account label")
	}
	if user.Name == "" {
		user.Name = label
	}
	credential, _ := json.Marshal(microsoftCredential{Refresh: access.Refresh, UserID: user.ID})
	// UserInfo's subject links both services within the same tenant context.
	// Display email and directory object ID are never used as a global sign-in key.
	return identity{subject.Sub, label, user.Name, string(credential)}, nil
}

func (a *Server) microsoftAccessToken(ctx context.Context, c Connection) (string, string, error) {
	stored, err := a.store.decrypt(c.token, c.Provider+":"+c.remoteID)
	var credential microsoftCredential
	if err != nil || json.Unmarshal([]byte(stored), &credential) != nil || credential.Refresh == "" || credential.UserID == "" {
		return "", "", &providerError{message: "Please reconnect this Microsoft account.", reconnect: true}
	}
	form := url.Values{"client_id": {a.cfg.MicrosoftClientID}, "client_secret": {a.cfg.MicrosoftClientSecret}, "refresh_token": {credential.Refresh}, "grant_type": {"refresh_token"}, "scope": {microsoftScopes(c.Provider)}}
	var access microsoftToken
	if err = a.microsoftRequest(ctx, "POST", microsoftAuthority(c.Provider)+"token", "", strings.NewReader(form.Encode()), true, &access); err != nil {
		return "", "", err
	}
	if access.Access == "" || !strings.EqualFold(access.Type, "Bearer") || !hasMicrosoftScope(access.Scope, microsoftScope(c.Provider)) {
		return "", "", &providerError{message: "Microsoft access no longer includes this app. Please reconnect.", reconnect: true}
	}
	// Microsoft rotates refresh tokens. Save the replacement before attempting a write.
	if access.Refresh != "" && access.Refresh != credential.Refresh {
		credential.Refresh = access.Refresh
		data, _ := json.Marshal(credential)
		encrypted := a.store.encrypt(string(data), c.Provider+":"+c.remoteID)
		if _, err = a.store.db.ExecContext(ctx, `UPDATE connections SET token=$1 WHERE id=$2`, encrypted, c.ID); err != nil {
			return "", "", err
		}
	}
	return access.Access, credential.UserID, nil
}

func (a *Server) setMicrosoftStatus(ctx context.Context, c Connection, target string) error {
	token, userID, err := a.microsoftAccessToken(ctx, c)
	if err != nil {
		return err
	}
	if c.Provider == "outlook" {
		return a.setOutlookStatus(ctx, c, target, token)
	}
	message := ""
	if target == "away" {
		message = "🌴 " + c.Message
	}
	body := map[string]any{"statusMessage": map[string]any{"message": map[string]string{"content": message, "contentType": "text"}}}
	// The documented response is an empty 200. No expiry means manual clearing.
	return a.microsoftRequest(ctx, "POST", "https://graph.microsoft.com/v1.0/users/"+url.PathEscape(userID)+"/presence/setStatusMessage", token, payload(body), false, nil)
}

type automaticReplies struct {
	Status   string `json:"status"`
	Audience string `json:"externalAudience"`
	Internal string `json:"internalReplyMessage"`
	External string `json:"externalReplyMessage"`
}

func (a *Server) setOutlookStatus(ctx context.Context, c Connection, target, token string) error {
	endpoint := "https://graph.microsoft.com/v1.0/me/mailboxSettings"
	patch := map[string]any{"status": "disabled"}
	if target == "away" {
		var settings struct {
			Replies *automaticReplies `json:"automaticRepliesSetting"`
		}
		if err := a.microsoftRequest(ctx, "GET", endpoint+"?$select=automaticRepliesSetting", token, nil, false, &settings); err != nil {
			return err
		}
		if settings.Replies == nil {
			return &providerError{message: "Outlook didn’t return automatic reply settings. Please retry."}
		}
		switch strings.ToLower(settings.Replies.Audience) {
		case "none", "contactsonly", "all":
		default:
			return &providerError{message: "Outlook didn’t confirm who should receive external replies. Please retry."}
		}
		message := "<html><body>" + strings.ReplaceAll(html.EscapeString(c.Message), "\n", "<br>") + "</body></html>"
		patch = map[string]any{"status": "alwaysEnabled", "externalAudience": settings.Replies.Audience, "internalReplyMessage": message, "externalReplyMessage": message}
	}
	var confirmed struct {
		Replies *automaticReplies `json:"automaticRepliesSetting"`
	}
	if err := a.microsoftRequest(ctx, "PATCH", endpoint, token, payload(map[string]any{"automaticRepliesSetting": patch}), false, &confirmed); err != nil {
		return err
	}
	if confirmed.Replies == nil || !strings.EqualFold(confirmed.Replies.Status, patch["status"].(string)) {
		return &providerError{message: "Outlook hasn’t confirmed the requested automatic replies. Please retry."}
	}
	if target == "away" && (!strings.EqualFold(confirmed.Replies.Audience, patch["externalAudience"].(string)) || confirmed.Replies.Internal == "" || confirmed.Replies.External == "") {
		return &providerError{message: "Outlook hasn’t confirmed the reply messages and recipient settings. Please retry."}
	}
	return nil
}
