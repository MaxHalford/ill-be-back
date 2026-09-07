package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL, AppURL, StaticDir           string
	EncryptionKey                            []byte
	GitHubClientID, GitHubClientSecret       string
	SlackClientID, SlackClientSecret         string
	GoogleClientID, GoogleClientSecret       string
	MicrosoftClientID, MicrosoftClientSecret string
	GoogleVerified                           bool
	HTTPClient                               *http.Client
	Development                              bool
}

type Server struct {
	cfg     Config
	store   *store
	handler http.Handler
}

func New(cfg Config) (*Server, error) {
	u, err := url.Parse(cfg.AppURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("APP_URL must be an origin, such as https://illbeback.example.com")
	}
	if !cfg.Development && u.Scheme != "https" {
		return nil, errors.New("production APP_URL must use HTTPS")
	}
	if len(cfg.EncryptionKey) != 32 {
		return nil, errors.New("encryption key must contain 32 bytes")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	s, err := openStore(cfg.DatabaseURL, cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	a := &Server{cfg: cfg, store: s}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/state", a.state)
	mux.HandleFunc("POST /api/status", a.changeStatus)
	mux.HandleFunc("POST /api/messages", a.messages)
	mux.HandleFunc("POST /api/logout", a.logout)
	mux.HandleFunc("DELETE /api/connections/{connection}", a.disconnect)
	mux.HandleFunc("DELETE /api/account", a.deleteAccount)
	mux.HandleFunc("GET /privacy", a.policy)
	mux.HandleFunc("GET /terms", a.policy)
	mux.HandleFunc("GET /auth/{provider}", a.oauthStart)
	mux.HandleFunc("GET /auth/{provider}/callback", a.oauthCallback)
	mux.HandleFunc("GET /", a.frontend)
	a.handler = a.security(mux)
	return a, nil
}

func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.handler.ServeHTTP(w, r) }
func (a *Server) Close() error                                     { return a.store.db.Close() }

func (a *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if !a.cfg.Development {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			devOrigin := a.cfg.Development && (origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173")
			if (origin != "" && origin != a.cfg.AppURL && !devOrigin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				problem(w, 403, "This request didn’t come from this app.")
				return
			}
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("request failed", "path", r.URL.Path)
				problem(w, 500, "Something went wrong. Please try again.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func jsonResponse(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, code int, message string) {
	jsonResponse(w, code, map[string]string{"error": message})
}
func internalError(w http.ResponseWriter, err error) {
	slog.Error("database operation failed", "error", err)
	problem(w, 500, "We couldn’t save that change. Please try again.")
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		problem(w, 400, "Please send a valid request.")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		problem(w, 400, "Please send a single request.")
		return false
	}
	return true
}

type session struct{ id, account, csrf string }

func (a *Server) cookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: "ibb_session", Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.AppURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (a *Server) session(w http.ResponseWriter, r *http.Request, create bool) (session, error) {
	var s session
	if cookie, err := r.Cookie("ibb_session"); err == nil {
		s.id = digest(cookie.Value)
		err = a.store.db.QueryRowContext(r.Context(), `SELECT COALESCE(account_id,''),csrf FROM sessions WHERE id=$1 AND expires>$2`, s.id, time.Now().Unix()).Scan(&s.account, &s.csrf)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return session{}, err
		}
	}
	if !create {
		return session{}, sql.ErrNoRows
	}
	raw := randomToken()
	s = session{id: digest(raw), csrf: randomToken()}
	_, err := a.store.db.ExecContext(r.Context(), `INSERT INTO sessions(id,csrf,expires) VALUES($1,$2,$3)`, s.id, s.csrf, time.Now().Add(30*24*time.Hour).Unix())
	if err != nil {
		return session{}, err
	}
	a.cookie(w, raw, 30*24*3600)
	return s, nil
}

func (a *Server) authenticated(w http.ResponseWriter, r *http.Request) (session, bool) {
	s, err := a.session(w, r, false)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && s.account == "") {
		problem(w, 401, "Please sign in to update your apps.")
		return s, false
	}
	if err != nil {
		internalError(w, err)
		return s, false
	}
	if subtle.ConstantTimeCompare([]byte(s.csrf), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		problem(w, 403, "Your session changed. Refresh the page and try again.")
		return s, false
	}
	return s, true
}

type appState struct {
	Authenticated bool            `json:"authenticated"`
	Name          string          `json:"name"`
	DesiredStatus string          `json:"desiredStatus"`
	ReturnAt      string          `json:"returnAt"`
	Connections   []Connection    `json:"connections"`
	Providers     map[string]bool `json:"providers"`
	CSRFToken     string          `json:"csrfToken"`
	GoogleTesting bool            `json:"googleTesting"`
}

func (a *Server) respondState(w http.ResponseWriter, r *http.Request, s session) {
	result := appState{Authenticated: s.account != "", DesiredStatus: "available", Connections: []Connection{}, CSRFToken: s.csrf, Providers: map[string]bool{"github": a.enabled("github"), "slack": a.enabled("slack"), "gmail": a.enabled("gmail"), "calendar": a.enabled("calendar"), "teams": a.enabled("teams"), "outlook": a.enabled("outlook")}}
	result.GoogleTesting = a.enabled("gmail") && !a.cfg.GoogleVerified
	if s.account != "" {
		err := a.store.db.QueryRowContext(r.Context(), `SELECT name,desired_status,return_at FROM accounts WHERE id=$1`, s.account).Scan(&result.Name, &result.DesiredStatus, &result.ReturnAt)
		if err != nil {
			internalError(w, err)
			return
		}
		result.Connections, err = a.store.connections(r.Context(), s.account)
		if err != nil {
			internalError(w, err)
			return
		}
		for i, c := range result.Connections {
			if c.Provider == "calendar" && c.Status == "away" {
				end, err := time.Parse(time.RFC3339, c.AwayUntil)
				if err != nil || !end.After(time.Now()) {
					result.Connections[i].Status = "unknown"
					result.Connections[i].Error = "Your Calendar absence ended. Choose a new return time or switch back."
				}
			}
		}
	}
	jsonResponse(w, 200, result)
}
func (a *Server) state(w http.ResponseWriter, r *http.Request) {
	s, err := a.session(w, r, true)
	if err != nil {
		internalError(w, err)
		return
	}
	a.respondState(w, r, s)
}
func (a *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if a.store.db.PingContext(ctx) != nil {
		problem(w, 503, "Database unavailable")
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "ok"})
}

func (a *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(a.cfg.StaticDir, "index.html"))
		return
	}
	if r.URL.Path != "/favicon.svg" && !strings.HasPrefix(r.URL.Path, "/assets/") && !strings.HasPrefix(r.URL.Path, "/art/") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.cfg.StaticDir, filepath.Clean("/"+r.URL.Path))
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFile(w, r, path)
}
