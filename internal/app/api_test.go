package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maxhalford/ill-be-back/internal/app"
)

// The HTTP API is the agreed seam. Only the external providers are simulated;
// requests exercise real OAuth, cookies, CSRF, persistence, and provider payloads.
type providerStub struct {
	mu             sync.Mutex
	githubID       int
	githubStatus   string
	slackStatus    string
	githubRequests []map[string]any
	slackRequests  []map[string]any
	started        chan struct{}
	unblock        chan struct{}
}

func (p *providerStub) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	response := ""
	switch r.URL.Host + r.URL.Path {
	case "github.com/login/oauth/access_token":
		response = `{"access_token":"test-github-secret","scope":"user"}`
	case "api.github.com/user":
		response = fmt.Sprintf(`{"id":%d,"login":"worker%d","name":"Test Worker"}`, p.githubID, p.githubID)
	case "slack.com/api/oauth.v2.access":
		response = `{"ok":true,"authed_user":{"id":"U1","access_token":"test-slack-secret","scope":"users.profile:write"}}`
	case "slack.com/api/auth.test":
		response = `{"ok":true,"user_id":"U1","team_id":"T1","user":"worker","team":"Test workspace"}`
	case "api.github.com/graphql":
		if p.started != nil {
			close(p.started)
			p.started = nil
			select {
			case <-p.unblock:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return nil, err
		}
		p.githubRequests = append(p.githubRequests, input)
		response = p.githubStatus
		if response == "" {
			v := input["variables"].(map[string]any)["input"].(map[string]any)
			data, _ := json.Marshal(map[string]any{"data": map[string]any{"changeUserStatus": map[string]any{"status": map[string]any{"message": v["message"], "indicatesLimitedAvailability": v["limitedAvailability"]}}}})
			response = string(data)
		}
	case "slack.com/api/users.profile.set":
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return nil, err
		}
		p.slackRequests = append(p.slackRequests, input)
		response = p.slackStatus
		if response == "" {
			data, _ := json.Marshal(map[string]any{"ok": true, "profile": input["profile"]})
			response = string(data)
		}
	default:
		return nil, fmt.Errorf("unexpected provider request: %s", r.URL)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
}

type testApp struct {
	server   *httptest.Server
	client   *http.Client
	provider *providerStub
}
type state struct {
	Authenticated bool             `json:"authenticated"`
	DesiredStatus string           `json:"desiredStatus"`
	Connections   []app.Connection `json:"connections"`
	CSRFToken     string           `json:"csrfToken"`
}

func setup(t *testing.T) *testApp {
	t.Helper()
	p := &providerStub{githubID: 1}
	a, err := app.New(app.Config{DatabaseURL: filepath.Join(t.TempDir(), "app.db"), EncryptionKey: bytes.Repeat([]byte{7}, 32), AppURL: "http://localhost:8000", Development: true,
		GitHubClientID: "github-id", GitHubClientSecret: "github-secret", SlackClientID: "slack-id", SlackClientSecret: "slack-secret", HTTPClient: &http.Client{Transport: p}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a)
	t.Cleanup(func() { server.Close(); a.Close() })
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return &testApp{server, client, p}
}

func (a *testApp) call(t *testing.T, method, path, csrf string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	r, err := http.NewRequest(method, a.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	response, err := a.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}
func readState(t *testing.T, data []byte) state {
	t.Helper()
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func (a *testApp) state(t *testing.T) state {
	t.Helper()
	code, data := a.call(t, "GET", "/api/state", "", nil)
	if code != 200 {
		t.Fatalf("state: %d %s", code, data)
	}
	return readState(t, data)
}
func (a *testApp) connect(t *testing.T, provider string) string {
	t.Helper()
	r, err := a.client.Get(a.server.URL + "/auth/" + provider)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	location, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Get("state") == "" {
		t.Fatalf("missing OAuth state: %s", location)
	}
	callback := "/auth/" + provider + "/callback?code=test-code&state=" + url.QueryEscape(location.Query().Get("state"))
	r, err = a.client.Get(a.server.URL + callback)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Header.Get("Location") != "/" {
		t.Fatalf("OAuth failed: %s", r.Header.Get("Location"))
	}
	return callback
}

func TestProviderMustConfirmAway(t *testing.T) {
	a := setup(t)
	a.connect(t, "github")
	a.provider.githubStatus = `{"data":{"changeUserStatus":{}}}`
	s := a.state(t)
	code, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("status: %d %s", code, data)
	}
	result := readState(t, data)
	if result.Connections[0].Status != "unknown" || result.Connections[0].Error == "" {
		t.Fatalf("an unconfirmed provider response must not show success: %+v", result.Connections[0])
	}
}

func TestAwayBackAndSavedMessages(t *testing.T) {
	a := setup(t)
	a.connect(t, "github")
	a.connect(t, "slack")
	s := a.state(t)
	code, data := a.call(t, "POST", "/api/messages", s.CSRFToken, map[string]any{"messages": map[string]string{"github": "On holiday", "slack": "Back next week"}})
	if code != 200 {
		t.Fatalf("save: %d %s", code, data)
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("away: %d %s", code, data)
	}
	result := readState(t, data)
	if len(result.Connections) != 2 {
		t.Fatalf("expected two connected apps: %s", data)
	}
	for _, c := range result.Connections {
		if c.Status != "away" || c.Error != "" {
			t.Fatalf("not away: %+v", c)
		}
	}
	github := a.provider.githubRequests[0]["variables"].(map[string]any)["input"].(map[string]any)
	if github["message"] != "On holiday" || github["emoji"] != ":palm_tree:" || github["limitedAvailability"] != true || github["expiresAt"] != nil {
		t.Fatalf("wrong GitHub status: %+v", github)
	}
	slack := a.provider.slackRequests[0]["profile"].(map[string]any)
	if slack["status_text"] != "Back next week" || slack["status_emoji"] != ":palm_tree:" || slack["status_expiration"] != float64(0) {
		t.Fatalf("wrong Slack status: %+v", slack)
	}
	a.call(t, "POST", "/api/messages", s.CSRFToken, map[string]any{"messages": map[string]string{"github": "Friday off"}})
	for _, c := range a.state(t).Connections {
		if c.Provider == "github" && (c.Message != "Friday off" || c.AppliedMessage != "On holiday") {
			t.Fatalf("saving a future message changed the applied message: %+v", c)
		}
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	if code != 200 {
		t.Fatalf("back: %d %s", code, data)
	}
	for _, c := range readState(t, data).Connections {
		if c.Status != "available" || c.Error != "" {
			t.Fatalf("not cleared: %+v", c)
		}
	}
	github = a.provider.githubRequests[1]["variables"].(map[string]any)["input"].(map[string]any)
	if github["message"] != "" || github["emoji"] != "" || github["limitedAvailability"] != false {
		t.Fatalf("GitHub away status not cleared: %+v", github)
	}
	slack = a.provider.slackRequests[1]["profile"].(map[string]any)
	if slack["status_text"] != "" || slack["status_emoji"] != "" {
		t.Fatalf("Slack away status not cleared: %+v", slack)
	}
	if bytes.Contains(data, []byte("test-github-secret")) || bytes.Contains(data, []byte("test-slack-secret")) {
		t.Fatal("provider token leaked through API")
	}
}

func TestPartialFailureKeepsSuccessAndCanRetry(t *testing.T) {
	a := setup(t)
	a.connect(t, "github")
	a.connect(t, "slack")
	a.provider.slackStatus = `{"ok":false,"error":"invalid_auth"}`
	s := a.state(t)
	code, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("partial result: %d %s", code, data)
	}
	result := readState(t, data)
	for _, c := range result.Connections {
		if c.Provider == "github" && (c.Status != "away" || c.Error != "") {
			t.Fatalf("successful GitHub update was lost: %+v", c)
		}
		if c.Provider == "slack" && (c.Status != "unknown" || !c.NeedsReconnect || c.Error == "") {
			t.Fatalf("Slack failure was hidden: %+v", c)
		}
	}
	a.provider.slackStatus = ""
	a.connect(t, "slack")
	s = a.state(t)
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("retry: %d %s", code, data)
	}
	for _, c := range readState(t, data).Connections {
		if c.Status != "away" || c.Error != "" || c.NeedsReconnect {
			t.Fatalf("retry did not recover: %+v", c)
		}
	}
}

func TestOAuthAndAccountIsolation(t *testing.T) {
	a := setup(t)
	anonymous := a.state(t)
	callback := a.connect(t, "github")
	first := a.state(t)
	if !first.Authenticated || first.CSRFToken == anonymous.CSRFToken {
		t.Fatal("OAuth must rotate the anonymous session")
	}
	r, err := a.client.Get(a.server.URL + callback)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Header.Get("Location") != "/?error=state" {
		t.Fatal("OAuth callback can be replayed")
	}
	a.connect(t, "slack")
	first = a.state(t)
	code, data := a.call(t, "POST", "/api/messages", first.CSRFToken, map[string]any{"messages": map[string]string{"github": "First user’s holiday"}})
	if code != 200 {
		t.Fatalf("save: %d %s", code, data)
	}
	firstClient := a.client
	jar, _ := cookiejar.New(nil)
	a.client = &http.Client{Jar: jar, CheckRedirect: firstClient.CheckRedirect}
	a.provider.githubID = 2
	a.connect(t, "github")
	second := a.state(t)
	if len(second.Connections) != 1 || second.Connections[0].Message != "Out of office" {
		t.Fatal("another account’s connections/preferences leaked")
	}
	code, _ = a.call(t, "POST", "/api/status", first.CSRFToken, map[string]string{"status": "away"})
	if code != 403 {
		t.Fatalf("another account’s CSRF token was accepted: %d", code)
	}
	r, err = a.client.Get(a.server.URL + "/auth/slack")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	location, _ := url.Parse(r.Header.Get("Location"))
	r, err = a.client.Get(a.server.URL + "/auth/slack/callback?code=test-code&state=" + url.QueryEscape(location.Query().Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Header.Get("Location") != "/?error=linked" {
		t.Fatal("a Slack connection can be stolen from another account")
	}
	a.client = firstClient
	first = a.state(t)
	if len(first.Connections) != 2 || first.Connections[0].Message != "First user’s holiday" {
		t.Fatal("first account’s data changed")
	}
	a.call(t, "POST", "/api/logout", first.CSRFToken, map[string]any{})
	if a.state(t).Authenticated {
		t.Fatal("sign-out did not clear session")
	}
	a.provider.githubID = 1
	a.connect(t, "github")
	if a.state(t).Connections[0].Message != "First user’s holiday" {
		t.Fatal("preferences lost after signing in again")
	}
}

func TestConcurrentSwitchesCannotInterleave(t *testing.T) {
	a := setup(t)
	a.connect(t, "github")
	s := a.state(t)
	started, unblock := make(chan struct{}), make(chan struct{})
	a.provider.started = started
	a.provider.unblock = unblock
	done := make(chan int, 1)
	go func() {
		code, _ := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
		done <- code
	}()
	<-started
	code, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	close(unblock)
	if code != 409 {
		t.Fatalf("overlapping switch was not rejected: %d %s", code, data)
	}
	if <-done != 200 {
		t.Fatal("first switch failed")
	}
	if a.state(t).Connections[0].Status != "away" {
		t.Fatal("concurrent switch corrupted the final state")
	}
}

func TestMutationValidationAndCSRF(t *testing.T) {
	a := setup(t)
	if code, _ := a.call(t, "POST", "/api/status", "", map[string]string{"status": "away"}); code != 401 {
		t.Fatalf("anonymous mutation: %d", code)
	}
	a.connect(t, "github")
	s := a.state(t)
	if code, _ := a.call(t, "POST", "/api/status", "", map[string]string{"status": "away"}); code != 403 {
		t.Fatalf("missing CSRF accepted: %d", code)
	}
	if code, _ := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "vacation"}); code != 400 {
		t.Fatalf("invalid status accepted: %d", code)
	}
	if code, _ := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away", "account": "someone-else"}); code != 400 {
		t.Fatalf("unexpected account accepted: %d", code)
	}
	for _, message := range []string{"   ", strings.Repeat("a", 81)} {
		if code, _ := a.call(t, "POST", "/api/messages", s.CSRFToken, map[string]any{"messages": map[string]string{"github": message}}); code != 400 {
			t.Fatalf("invalid message accepted: %d", code)
		}
	}
	r, _ := http.NewRequest("POST", a.server.URL+"/api/status", strings.NewReader(`{"status":"away"}`))
	r.Header.Set("X-CSRF-Token", s.CSRFToken)
	r.Header.Set("Origin", "https://attacker.example")
	response, err := a.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("cross-origin mutation accepted: %d", response.StatusCode)
	}
	if len(a.provider.githubRequests) != 0 {
		t.Fatal("rejected requests changed the external status")
	}
}

func TestDisconnectClearsStatusAndLastConnectionDeletesAccount(t *testing.T) {
	a := setup(t)
	a.connect(t, "github")
	s := a.state(t)
	a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	code, data := a.call(t, "DELETE", "/api/connections/github", s.CSRFToken, map[string]any{})
	if code != 200 || readState(t, data).Authenticated {
		t.Fatalf("last disconnect did not remove account: %d %s", code, data)
	}
	request := a.provider.githubRequests[1]["variables"].(map[string]any)["input"].(map[string]any)
	if request["message"] != "" || request["limitedAvailability"] != false {
		t.Fatal("disconnect did not clear away status")
	}
	a.connect(t, "github")
	if a.state(t).Connections[0].Status != "unknown" {
		t.Fatal("deleted account returned old connection state")
	}
}
