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
	"time"

	"github.com/maxhalford/ill-be-back/internal/app"
)

const gmailScope = "https://www.googleapis.com/auth/gmail.settings.basic"
const calendarScope = "https://www.googleapis.com/auth/calendar.events.owned"

type googleStub struct {
	mu                            sync.Mutex
	subject, scopes, refreshError string
	refreshes                     int
	vacation                      map[string]map[string]any
	writes                        []string
	events                        map[string]map[string]any
	eventIDs                      []string
	loseEventResponse             bool
}

func (g *googleStub) RoundTrip(r *http.Request) (*http.Response, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var result any
	code := 200
	switch r.URL.Host + r.URL.Path {
	case "oauth2.googleapis.com/token":
		r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			g.refreshes++
			if g.refreshError != "" {
				code = 400
				result = map[string]string{"error": g.refreshError}
			} else {
				result = map[string]any{"access_token": "access:" + strings.TrimPrefix(r.Form.Get("refresh_token"), "refresh:"), "expires_in": 3600, "token_type": "Bearer"}
			}
		} else {
			result = map[string]any{"access_token": "access:" + g.subject, "refresh_token": "refresh:" + g.subject, "expires_in": 3600, "token_type": "Bearer", "scope": g.scopes}
		}
	case "openidconnect.googleapis.com/v1/userinfo":
		subject := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer access:")
		result = map[string]any{"sub": subject, "email": subject + "@example.com", "email_verified": true}
	case "gmail.googleapis.com/gmail/v1/users/me/settings/vacation":
		subject := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer access:")
		if r.Method == "PUT" {
			var v map[string]any
			if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
				return nil, err
			}
			g.vacation[subject] = v
			g.writes = append(g.writes, subject)
		}
		result = g.vacation[subject]
		if result == nil {
			result = map[string]any{"enableAutoReply": false}
		}
	default:
		if r.URL.Host == "www.googleapis.com" && strings.HasPrefix(r.URL.Path, "/calendar/v3/calendars/primary/events") {
			key := strings.TrimPrefix(r.URL.Path, "/calendar/v3/calendars/primary/events/")
			if g.events == nil {
				g.events = map[string]map[string]any{}
			}
			switch r.Method {
			case "POST":
				var event map[string]any
				_ = json.NewDecoder(r.Body).Decode(&event)
				key = event["id"].(string)
				if g.events[key] != nil {
					code = 409
					result = map[string]any{"error": map[string]any{"code": 409}}
				} else {
					g.events[key] = event
					g.eventIDs = append(g.eventIDs, key)
					result = event
					if g.loseEventResponse {
						g.loseEventResponse = false
						return nil, fmt.Errorf("lost reply after successful insert")
					}
				}
			case "GET":
				if event := g.events[key]; event != nil {
					result = event
				} else {
					code = 404
					result = map[string]any{}
				}
			case "PATCH":
				if event := g.events[key]; event != nil {
					var patch map[string]any
					_ = json.NewDecoder(r.Body).Decode(&patch)
					for k, v := range patch {
						event[k] = v
					}
					result = event
				} else {
					code = 404
					result = map[string]any{}
				}
			case "DELETE":
				delete(g.events, key)
				code = 204
			default:
				return nil, fmt.Errorf("unexpected calendar method %s", r.Method)
			}
		} else {
			return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	}
	data, _ := json.Marshal(result)
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
}
func setupGoogle(t *testing.T) (*testApp, *googleStub) {
	t.Helper()
	g := &googleStub{subject: "one", scopes: "openid email " + gmailScope + " " + calendarScope, vacation: map[string]map[string]any{"one": {"enableAutoReply": false, "restrictToContacts": true, "restrictToDomain": true, "responseBodyHtml": "old message", "endTime": "1"}}}
	a, err := app.New(app.Config{DatabaseURL: filepath.Join(t.TempDir(), "app.db"), EncryptionKey: bytes.Repeat([]byte{7}, 32), AppURL: "http://localhost:8000", Development: true, GoogleClientID: "google-id", GoogleClientSecret: "google-secret", HTTPClient: &http.Client{Transport: g}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a)
	t.Cleanup(func() { server.Close(); a.Close() })
	jar, _ := cookiejar.New(nil)
	return &testApp{server, &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, nil}, g
}
func TestGmailVacationWithMultipleAccountsAndRefresh(t *testing.T) {
	a, g := setupGoogle(t)
	a.connect(t, "gmail")
	first := a.state(t).Connections[0].ID
	g.subject = "two"
	a.connect(t, "gmail")
	s := a.state(t)
	if len(s.Connections) != 2 {
		t.Fatal("expected two Gmail accounts")
	}
	code, data := a.call(t, "POST", "/api/messages", s.CSRFToken, map[string]any{"messages": map[string]string{first: "On holiday"}})
	if code != 200 {
		t.Fatalf("messages: %d %s", code, data)
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("away: %d %s", code, data)
	}
	for _, c := range readState(t, data).Connections {
		if c.Status != "away" {
			t.Fatalf("not away: %+v", c)
		}
	}
	v := g.vacation["one"]
	if v["enableAutoReply"] != true || v["responseBodyPlainText"] != "On holiday" || v["restrictToContacts"] != true || v["restrictToDomain"] != true || v["responseBodyHtml"] != "" || v["endTime"] != nil {
		t.Fatalf("wrong vacation settings: %+v", v)
	}
	if g.vacation["two"]["responseBodyPlainText"] != "Out of office" || g.refreshes != 2 {
		t.Fatal("account credentials or refresh incorrect")
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	if code != 200 || g.vacation["one"]["enableAutoReply"] != false || g.vacation["two"]["enableAutoReply"] != false {
		t.Fatalf("back: %d %s", code, data)
	}
	if bytes.Contains(data, []byte("refresh:")) || bytes.Contains(data, []byte("access:")) {
		t.Fatal("tokens exposed")
	}
	g.refreshError = "invalid_grant"
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"})
	if code != 200 {
		t.Fatalf("expired grant: %d %s", code, data)
	}
	for _, c := range readState(t, data).Connections {
		if c.Status != "unknown" || !c.NeedsReconnect {
			t.Fatalf("revocation hidden: %+v", c)
		}
	}
}

func TestGoogleIdentityIsSharedAcrossGmailAndCalendar(t *testing.T) {
	a, g := setupGoogle(t)
	a.connect(t, "gmail")
	first := a.state(t)
	a.call(t, "POST", "/api/logout", first.CSRFToken, map[string]any{})
	a.connect(t, "calendar")
	s := a.state(t)
	if len(s.Connections) != 2 {
		t.Fatal("same Google identity created separate product accounts")
	}
	firstClient := a.client
	jar, _ := cookiejar.New(nil)
	a.client = &http.Client{Jar: jar, CheckRedirect: firstClient.CheckRedirect}
	g.subject = "other"
	a.connect(t, "calendar")
	g.subject = "one"
	r, err := a.client.Get(a.server.URL + "/auth/gmail")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	location, _ := url.Parse(r.Header.Get("Location"))
	if location.Query().Get("scope") != "openid email "+gmailScope || location.Query().Get("access_type") != "offline" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("wrong Gmail OAuth permissions")
	}
	r, err = a.client.Get(a.server.URL + "/auth/gmail/callback?code=test&state=" + location.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Header.Get("Location") != "/?error=linked" {
		t.Fatal("Google identity transferred between owners")
	}
	a.client = firstClient
	if len(a.state(t).Connections) != 2 {
		t.Fatal("original account changed")
	}
}

func TestCalendarRetryDoesNotDuplicateAndBackOnlyClearsOwnEvent(t *testing.T) {
	a, g := setupGoogle(t)
	a.connect(t, "calendar")
	s := a.state(t)
	if code, _ := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away"}); code != 400 {
		t.Fatalf("missing return time accepted: %d", code)
	}
	until := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)
	g.events = map[string]map[string]any{"unrelated": {"id": "unrelated", "summary": "Customer meeting"}}
	g.loseEventResponse = true
	code, data := a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away", "returnAt": until})
	if code != 200 || readState(t, data).Connections[0].Status != "unknown" {
		t.Fatalf("lost insert reply falsely confirmed: %d %s", code, data)
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away", "returnAt": until})
	if code != 200 || readState(t, data).Connections[0].Status != "away" || len(g.eventIDs) != 1 {
		t.Fatalf("retry duplicated event or failed: %d %s", code, data)
	}
	event := g.events[g.eventIDs[0]]
	if event["eventType"] != "outOfOffice" || event["transparency"] != "opaque" || event["end"].(map[string]any)["dateTime"] != until || event["outOfOfficeProperties"].(map[string]any)["autoDeclineMode"] != "declineNone" {
		t.Fatalf("wrong calendar event: %+v", event)
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	if code != 200 || readState(t, data).Connections[0].Status != "available" || len(g.events) != 1 || g.events["unrelated"] == nil {
		t.Fatalf("back removed wrong event: %d %s", code, data)
	}
	code, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "away", "returnAt": until})
	if code != 200 || len(g.eventIDs) != 2 || g.eventIDs[0] == g.eventIDs[1] {
		t.Fatalf("new absence reused deleted event: %d %s", code, data)
	}
}

func TestDisconnectedGoogleIdentityCanNoLongerSignIntoAccount(t *testing.T) {
	a, g := setupGoogle(t)
	a.connect(t, "gmail")
	a.connect(t, "calendar")
	g.subject = "two"
	a.connect(t, "gmail")
	s := a.state(t)
	for _, c := range s.Connections {
		if c.Label == "one@example.com" {
			code, data := a.call(t, "DELETE", "/api/connections/"+c.ID, s.CSRFToken, map[string]any{})
			if code != 200 {
				t.Fatalf("disconnect: %d %s", code, data)
			}
		}
	}
	a.call(t, "POST", "/api/logout", s.CSRFToken, map[string]any{})
	g.subject = "one"
	a.connect(t, "gmail")
	s = a.state(t)
	if len(s.Connections) != 1 || s.Connections[0].Label != "one@example.com" {
		t.Fatal("removed Google identity retained access to original account")
	}
}

func TestGoogleMissingScopeCannotCreateAccount(t *testing.T) {
	a, g := setupGoogle(t)
	g.scopes = "openid email"
	r, err := a.client.Get(a.server.URL + "/auth/gmail")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	location, _ := url.Parse(r.Header.Get("Location"))
	r, err = a.client.Get(a.server.URL + "/auth/gmail/callback?code=test&state=" + location.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Header.Get("Location") != "/?error=oauth" || a.state(t).Authenticated {
		t.Fatal("missing required Gmail scope was accepted")
	}
}

func TestDeleteAccountWorksWithRevokedGoogleAccess(t *testing.T) {
	a, g := setupGoogle(t)
	a.connect(t, "gmail")
	s := a.state(t)
	g.refreshError = "invalid_grant"
	if code, _ := a.call(t, "DELETE", "/api/account", "", map[string]any{}); code != 403 {
		t.Fatal("deletion accepted without CSRF")
	}
	code, data := a.call(t, "DELETE", "/api/account", s.CSRFToken, map[string]any{})
	if code != 200 || readState(t, data).Authenticated {
		t.Fatalf("delete failed: %d %s", code, data)
	}
	a.connect(t, "gmail")
	fresh := a.state(t)
	if len(fresh.Connections) != 1 || fresh.Connections[0].ID == s.Connections[0].ID {
		t.Fatal("deleted connection survived fresh sign-in")
	}
}

func TestCalendarCanReplaceDeletedEventWithoutTouchingOthers(t *testing.T) {
	a, g := setupGoogle(t)
	a.connect(t, "calendar")
	s := a.state(t)
	input := map[string]string{"status": "away", "returnAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	_, data := a.call(t, "POST", "/api/status", s.CSRFToken, input)
	if readState(t, data).Connections[0].Status != "away" {
		t.Fatalf("away failed: %s", data)
	}
	old := g.eventIDs[0]
	g.events[old] = map[string]any{"id": old, "status": "cancelled"}
	_, data = a.call(t, "POST", "/api/status", s.CSRFToken, input)
	if readState(t, data).Connections[0].Status != "away" || len(g.eventIDs) != 2 {
		t.Fatalf("cancelled event prevented new absence: %s", data)
	}
	// If an event loses its ownership marker, leave it untouched and report failure.
	current := g.eventIDs[1]
	g.events[current]["extendedProperties"] = map[string]any{}
	_, data = a.call(t, "POST", "/api/status", s.CSRFToken, map[string]string{"status": "available"})
	if readState(t, data).Connections[0].Status != "unknown" || g.events[current] == nil {
		t.Fatalf("unowned event was removed: %s", data)
	}
}
