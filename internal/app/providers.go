package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type providerError struct {
	message   string
	reconnect bool
}

func (e *providerError) Error() string { return e.message }

func (a *Server) providerRequest(ctx context.Context, method, endpoint, token string, body io.Reader, form bool, out any) error {
	r, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "IllBeBack/1.0")
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
		return &providerError{message: "We couldn’t confirm the update. Please try again."}
	}
	defer response.Body.Close()
	if response.StatusCode == 429 || (response.StatusCode == 403 && response.Header.Get("X-RateLimit-Remaining") == "0") {
		return &providerError{message: "This app is asking us to slow down. Please try again in a minute."}
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return &providerError{message: "Your connection needs refreshing. Please reconnect this app.", reconnect: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &providerError{message: "This app couldn’t confirm the update. Please try again."}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return &providerError{message: "This app sent an unexpected response. Please try again."}
	}
	return nil
}

func payload(v any) io.Reader { data, _ := json.Marshal(v); return bytes.NewReader(data) }
func slackError(ok bool, code string) error {
	if ok {
		return nil
	}
	switch code {
	case "invalid_auth", "token_revoked", "token_expired", "account_inactive", "missing_scope", "not_authed":
		return &providerError{message: "Your Slack connection needs refreshing. Please reconnect.", reconnect: true}
	case "ratelimited":
		return &providerError{message: "Slack is asking us to slow down. Please try again in a minute."}
	case "no_permission", "not_allowed_token_type", "enterprise_is_restricted":
		return &providerError{message: "Your workspace didn’t allow this update. Check your app permissions.", reconnect: true}
	default:
		return &providerError{message: "Slack couldn’t confirm the update. Please try again."}
	}
}

func (a *Server) setStatus(ctx context.Context, c Connection, target string) error {
	token, err := a.store.decrypt(c.token, c.Provider+":"+c.remoteID)
	if err != nil {
		return &providerError{message: "This connection needs refreshing. Please reconnect.", reconnect: true}
	}
	message, emoji := "", ""
	if target == "away" {
		message, emoji = c.Message, ":palm_tree:"
	}
	if c.Provider == "slack" {
		var response struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		body := map[string]any{"profile": map[string]any{"status_text": message, "status_emoji": emoji, "status_expiration": 0}}
		err = a.providerRequest(ctx, "POST", "https://slack.com/api/users.profile.set", token, payload(body), false, &response)
		if err != nil {
			return err
		}
		return slackError(response.OK, response.Error)
	}
	var response struct {
		Data struct {
			Change *struct {
				Status json.RawMessage `json:"status"`
			} `json:"changeUserStatus"`
		} `json:"data"`
		Errors []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	body := map[string]any{
		"query":     `mutation SetAvailability($input: ChangeUserStatusInput!) { changeUserStatus(input: $input) { status { message indicatesLimitedAvailability } } }`,
		"variables": map[string]any{"input": map[string]any{"message": message, "emoji": emoji, "limitedAvailability": target == "away", "expiresAt": nil, "organizationId": nil}},
	}
	if err = a.providerRequest(ctx, "POST", "https://api.github.com/graphql", token, payload(body), false, &response); err != nil {
		return err
	}
	if len(response.Errors) > 0 {
		reconnect := response.Errors[0].Type == "FORBIDDEN" || response.Errors[0].Type == "UNAUTHORIZED"
		message := "GitHub couldn’t confirm the update. Please try again."
		if reconnect {
			message = "GitHub didn’t allow this update. Please reconnect your account."
		}
		return &providerError{message: message, reconnect: reconnect}
	}
	if response.Data.Change == nil {
		return &providerError{message: "GitHub sent an unexpected response. Please try again."}
	}
	var confirmed struct {
		Message string `json:"message"`
		Limited bool   `json:"indicatesLimitedAvailability"`
	}
	status := response.Data.Change.Status
	if len(status) == 0 || (target == "away" && string(status) == "null") || json.Unmarshal(status, &confirmed) != nil || confirmed.Message != message || confirmed.Limited != (target == "away") {
		return &providerError{message: "GitHub hasn’t confirmed the requested status. Please try again."}
	}
	return nil
}

type identity struct{ remoteID, label, name, token string }

func (a *Server) exchange(ctx context.Context, provider, code, verifier string) (identity, error) {
	callback := a.cfg.AppURL + "/auth/" + provider + "/callback"
	form := url.Values{"code": {code}, "redirect_uri": {callback}}
	if provider == "github" {
		form.Set("client_id", a.cfg.GitHubClientID)
		form.Set("client_secret", a.cfg.GitHubClientSecret)
		form.Set("code_verifier", verifier)
		var access struct {
			Token string `json:"access_token"`
			Error string `json:"error"`
			Scope string `json:"scope"`
		}
		if err := a.providerRequest(ctx, "POST", "https://github.com/login/oauth/access_token", "", strings.NewReader(form.Encode()), true, &access); err != nil {
			return identity{}, err
		}
		if access.Token == "" || access.Error != "" || !hasScope(access.Scope, "user") {
			return identity{}, errors.New("GitHub did not grant status permission")
		}
		var user struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Name  string `json:"name"`
		}
		if err := a.providerRequest(ctx, "GET", "https://api.github.com/user", access.Token, nil, false, &user); err != nil {
			return identity{}, err
		}
		if user.ID == 0 || user.Login == "" {
			return identity{}, errors.New("missing GitHub identity")
		}
		if user.Name == "" {
			user.Name = user.Login
		}
		return identity{strconv.FormatInt(user.ID, 10), "@" + user.Login, user.Name, access.Token}, nil
	}
	form.Set("client_id", a.cfg.SlackClientID)
	form.Set("client_secret", a.cfg.SlackClientSecret)
	var access struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		AuthedUser struct {
			ID      string `json:"id"`
			Token   string `json:"access_token"`
			Scope   string `json:"scope"`
			Refresh string `json:"refresh_token"`
		} `json:"authed_user"`
	}
	if err := a.providerRequest(ctx, "POST", "https://slack.com/api/oauth.v2.access", "", strings.NewReader(form.Encode()), true, &access); err != nil {
		return identity{}, err
	}
	if err := slackError(access.OK, access.Error); err != nil {
		return identity{}, err
	}
	if access.AuthedUser.Token == "" || !hasScope(access.AuthedUser.Scope, "users.profile:write") {
		return identity{}, errors.New("Slack did not grant a user status token")
	}
	// This release uses non-rotating user tokens. Fail clearly instead of storing a token
	// that would silently expire hours later; installation instructions disable rotation.
	if access.AuthedUser.Refresh != "" {
		return identity{}, errors.New("Slack token rotation must be disabled for this release")
	}
	var user struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		UserID string `json:"user_id"`
		TeamID string `json:"team_id"`
		User   string `json:"user"`
		Team   string `json:"team"`
	}
	if err := a.providerRequest(ctx, "POST", "https://slack.com/api/auth.test", access.AuthedUser.Token, nil, false, &user); err != nil {
		return identity{}, err
	}
	if err := slackError(user.OK, user.Error); err != nil {
		return identity{}, err
	}
	if user.UserID == "" || user.TeamID == "" || user.UserID != access.AuthedUser.ID {
		return identity{}, errors.New("invalid Slack identity")
	}
	return identity{user.TeamID + ":" + user.UserID, user.Team, user.User, access.AuthedUser.Token}, nil
}

func hasScope(scopes, required string) bool {
	for _, s := range strings.FieldsFunc(scopes, func(r rune) bool { return r == ',' || r == ' ' }) {
		if s == required {
			return true
		}
	}
	return false
}
