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

type microsoftStub struct {
	mu                            sync.Mutex
	subject, scopes, refreshError string
	teamsCode                     int
	mailboxResponse               string
	refreshes                     []string
	teams                         map[string]map[string]any
	mailboxes                     map[string]map[string]any
	patches                       []map[string]any
}

func (m *microsoftStub) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	code := 200
	var result any
	subject := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer access:")
	switch {
	case r.URL.Host == "login.microsoftonline.com" && strings.HasSuffix(r.URL.Path, "/token"):
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		if r.Form.Get("client_id") != "microsoft-id" || r.Form.Get("client_secret") != "microsoft-secret" {
			return nil, fmt.Errorf("missing client credentials")
		}
		scope := r.Form.Get("scope")
		if strings.Contains(scope, ".default") || (strings.Contains(scope, "Presence.ReadWrite") && strings.Contains(scope, "MailboxSettings.ReadWrite")) {
			return nil, fmt.Errorf("excessive permission request")
		}
		subject = m.subject
		refresh := "refresh:" + subject
		if r.Form.Get("grant_type") == "refresh_token" {
			refresh = r.Form.Get("refresh_token")
			m.refreshes = append(m.refreshes, refresh)
			subject = strings.Split(strings.TrimPrefix(refresh, "refresh:"), ":")[0]
			if m.refreshError != "" {
				code = 400
				result = map[string]string{"error": m.refreshError, "error_description": "private provider detail"}
				break
			}
			refresh += ":rotated"
		} else if r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") == "" {
			return nil, fmt.Errorf("missing PKCE or callback")
		}
		if m.scopes != "" {
			scope = m.scopes
		}
		result = map[string]string{"access_token": "access:" + subject, "refresh_token": refresh, "scope": scope, "token_type": "Bearer"}
	case r.URL.Host == "graph.microsoft.com" && r.URL.Path == "/oidc/userinfo":
		result = map[string]string{"sub": subject}
	case r.URL.Host == "graph.microsoft.com" && r.URL.Path == "/v1.0/me":
		result = map[string]string{"id": "directory-" + subject, "displayName": subject, "mail": "same-email@example.com"}
	case r.URL.Host == "graph.microsoft.com" && strings.HasSuffix(r.URL.Path, "/presence/setStatusMessage"):
		if r.Method != "POST" || r.URL.Path != "/v1.0/users/directory-"+subject+"/presence/setStatusMessage" {
			return nil, fmt.Errorf("wrong Teams identity or method")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		m.teams[subject] = body
		if m.teamsCode != 0 {
			code = m.teamsCode
		}
		if code != 200 {
			result = map[string]any{"error": map[string]string{"code": "Forbidden", "message": "private provider detail"}}
		}
	case r.URL.Host == "graph.microsoft.com" && r.URL.Path == "/v1.0/me/mailboxSettings":
		if m.mailboxes[subject] == nil {
			m.mailboxes[subject] = map[string]any{"status": "scheduled", "externalAudience": "contactsOnly", "internalReplyMessage": "old internal", "externalReplyMessage": "old external", "scheduledEndDateTime": map[string]string{"dateTime": "2030-01-01T00:00:00", "timeZone": "UTC"}}
		}
		if r.Method == "PATCH" {
			var body map[string]map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return nil, err
			}
			if len(body) != 1 || body["automaticRepliesSetting"] == nil {
				return nil, fmt.Errorf("unexpected mailbox settings changed")
			}
			patch := body["automaticRepliesSetting"]
			m.patches = append(m.patches, patch)
			for key, value := range patch {
				m.mailboxes[subject][key] = value
			}
			if m.mailboxResponse != "" {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(m.mailboxResponse)), Request: r}, nil
			}
		} else if r.Method != "GET" || r.URL.Query().Get("$select") != "automaticRepliesSetting" {
			return nil, fmt.Errorf("unnecessary mailbox read")
		}
		result = map[string]any{"automaticRepliesSetting": m.mailboxes[subject]}
	default:
		return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
	}
	var data []byte
	if result != nil {
		data, _ = json.Marshal(result)
	}
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
}

func setupMicrosoft(t *testing.T) (*testApp, *microsoftStub) {
	t.Helper()
	m := &microsoftStub{subject: "one", teams: map[string]map[string]any{}, mailboxes: map[string]map[string]any{}}
	a, err := app.New(app.Config{DatabaseURL: filepath.Join(t.TempDir(), "app.db"), EncryptionKey: bytes.Repeat([]byte{7}, 32), AppURL: "http://localhost:8000", Development: true, MicrosoftClientID: "microsoft-id", MicrosoftClientSecret: "microsoft-secret", HTTPClient: &http.Client{Transport: m}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a)
	t.Cleanup(func() { server.Close(); a.Close() })
	jar, _ := cookiejar.New(nil)
	return &testApp{server, &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, nil}, m
}

func microsoftCallback(t *testing.T, a *testApp, provider string) string {
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
	q := location.Query()
	wantAuthority, wantScope, otherScope := "/common/", "MailboxSettings.ReadWrite", "Presence.ReadWrite"
	if provider == "teams" {
		wantAuthority, wantScope, otherScope = "/organizations/", "Presence.ReadWrite", "MailboxSettings.ReadWrite"
	}
	if location.Host != "login.microsoftonline.com" || !strings.HasPrefix(location.Path, wantAuthority) || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), wantScope) || strings.Contains(q.Get("scope"), otherScope) || !strings.Contains(q.Get("scope"), "offline_access") {
		t.Fatalf("wrong Microsoft OAuth request: %s", location)
	}
	r, err = a.client.Get(a.server.URL + "/auth/" + provider + "/callback?code=test&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return r.Header.Get("Location")
}

func TestMicrosoftAwayBackAndCredentialRotation(t *testing.T) {
	a, m := setupMicrosoft(t)
	for _, p := range []string{"teams", "outlook"} {
		if got := microsoftCallback(t, a, p); got != "/" {
			t.Fatalf("connect %s: %s", p, got)
		}
	}
	s := a.state(t)
	messages := map[string]string{}
	for _, c := range s.Connections {
		messages[c.ID] = "Away <until> Monday & Tuesday\nContact Alex"
	}
	if code, data := a.call(t, "POST", "/api/messages", s.CSRFToken, map[string]any{"messages": messages}); code != 200 {
		t.Fatalf("messages: %d %s", code, data)
	}
	code, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("away: %d %s", code, data)
	}
	for _, c := range readState(t, data).Connections {
		if c.Status != "away" {
			t.Fatalf("away failed: %+v", c)
		}
	}
	status := m.teams["one"]["statusMessage"].(map[string]any)
	if len(status) != 1 || status["message"].(map[string]any)["content"] != "🌴 Away <until> Monday & Tuesday\nContact Alex" {
		t.Fatalf("wrong Teams status: %+v", status)
	}
	replies := m.mailboxes["one"]
	wantBody := "<html><body>Away &lt;until&gt; Monday &amp; Tuesday<br>Contact Alex</body></html>"
	if replies["status"] != "alwaysEnabled" || replies["externalAudience"] != "contactsOnly" || replies["internalReplyMessage"] != wantBody || replies["externalReplyMessage"] != wantBody {
		t.Fatalf("wrong replies: %+v", replies)
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	if code != 200 {
		t.Fatalf("back: %d %s", code, data)
	}
	for _, c := range readState(t, data).Connections {
		if c.Status != "available" {
			t.Fatalf("back failed: %+v", c)
		}
	}
	if m.teams["one"]["statusMessage"].(map[string]any)["message"].(map[string]any)["content"] != "" || replies["status"] != "disabled" || len(m.patches[1]) != 1 {
		t.Fatal("back did not clear or changed unrelated settings")
	}
	if len(m.refreshes) != 4 || m.refreshes[2] != "refresh:one:rotated" || m.refreshes[3] != "refresh:one:rotated" {
		t.Fatalf("rotated credentials not used: %v", m.refreshes)
	}
	if bytes.Contains(data, []byte("refresh:")) || bytes.Contains(data, []byte("access:")) {
		t.Fatal("credentials exposed")
	}
}

func TestMicrosoftIdentityOwnershipAndDisconnection(t *testing.T) {
	a, m := setupMicrosoft(t)
	a.connect(t, "teams")
	s := a.state(t)
	a.call(t, "POST", "/api/logout", s.CSRFToken, nil)
	a.connect(t, "outlook")
	if len(a.state(t).Connections) != 2 {
		t.Fatal("services created separate accounts")
	}
	a.connect(t, "outlook")
	if len(a.state(t).Connections) != 2 {
		t.Fatal("reconnection duplicated account")
	}
	firstClient := a.client
	jar, _ := cookiejar.New(nil)
	a.client = &http.Client{Jar: jar, CheckRedirect: firstClient.CheckRedirect}
	m.subject = "two"
	a.connect(t, "outlook")
	if len(a.state(t).Connections) != 1 {
		t.Fatal("matching display email merged unrelated identities")
	}
	m.subject = "one"
	if got := microsoftCallback(t, a, "teams"); got != "/?error=linked" {
		t.Fatalf("cross-account identity transfer: %s", got)
	}
	a.client = firstClient
	original := a.state(t).Connections
	m.subject = "three"
	a.connect(t, "outlook")
	s = a.state(t)
	// Remove subject one, leaving subject three attached to the original account.
	for _, c := range original {
		code, data := a.call(t, "DELETE", "/api/connections/"+c.ID, s.CSRFToken, nil)
		if code != 200 {
			t.Fatalf("disconnect: %d %s", code, data)
		}
	}
	a.call(t, "POST", "/api/logout", s.CSRFToken, nil)
	m.subject = "one"
	a.connect(t, "teams")
	if len(a.state(t).Connections) != 1 {
		t.Fatal("removed identity retained sign-in access")
	}
}

func TestMicrosoftPermissionFailuresAndPartialSuccess(t *testing.T) {
	a, m := setupMicrosoft(t)
	m.scopes = "openid User.Read"
	if got := microsoftCallback(t, a, "teams"); got != "/?error=oauth" || a.state(t).Authenticated {
		t.Fatal("missing scope accepted")
	}
	m.scopes = ""
	a.connect(t, "teams")
	a.connect(t, "outlook")
	s := a.state(t)
	m.teamsCode = 403
	_, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	for _, c := range readState(t, data).Connections {
		if c.Provider == "teams" && (c.Status != "unknown" || !c.NeedsReconnect || !strings.Contains(c.Error, "administrator")) {
			t.Fatalf("approval failure hidden: %+v", c)
		}
		if c.Provider == "outlook" && c.Status != "away" {
			t.Fatalf("partial success lost: %+v", c)
		}
	}
	m.refreshError = "invalid_grant"
	_, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	for _, c := range readState(t, data).Connections {
		if c.Status != "unknown" || !c.NeedsReconnect {
			t.Fatalf("revocation hidden: %+v", c)
		}
	}
	if bytes.Contains(data, []byte("private provider detail")) {
		t.Fatal("provider error leaked")
	}
	if code, data := a.call(t, "DELETE", "/api/account", s.CSRFToken, nil); code != 200 || readState(t, data).Authenticated {
		t.Fatalf("revoked credentials prevented deletion: %d %s", code, data)
	}
}

func TestOutlookMalformedConfirmationIsNotSuccess(t *testing.T) {
	for _, reply := range []string{"null", "{}", `{"automaticRepliesSetting":{"status":"disabled"}}`} {
		t.Run(reply, func(t *testing.T) {
			a, m := setupMicrosoft(t)
			a.connect(t, "outlook")
			m.mailboxResponse = reply
			_, data := a.call(t, "POST", "/api/status", a.state(t).CSRFToken, map[string]string{"status": "away"})
			if readState(t, data).Connections[0].Status != "unknown" {
				t.Fatalf("malformed success accepted: %s", data)
			}
		})
	}
}
