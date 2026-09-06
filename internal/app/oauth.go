package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"
)

func (a *Server) enabled(p string) bool {
	return (p == "github" && a.cfg.GitHubClientID != "" && a.cfg.GitHubClientSecret != "") || (p == "slack" && a.cfg.SlackClientID != "" && a.cfg.SlackClientSecret != "")
}
func (a *Server) oauthError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/?error="+code, http.StatusSeeOther)
}

func (a *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("provider")
	if !a.enabled(p) {
		a.oauthError(w, r, "unavailable")
		return
	}
	s, err := a.session(w, r, true)
	if err != nil {
		internalError(w, err)
		return
	}
	state, verifier := randomToken(), randomToken()+randomToken()
	_, err = a.store.db.ExecContext(r.Context(), `UPDATE sessions SET oauth_state=$1,oauth_provider=$2,oauth_verifier=$3,oauth_expires=$4 WHERE id=$5`, digest(state), p, verifier, time.Now().Add(10*time.Minute).Unix(), s.id)
	if err != nil {
		internalError(w, err)
		return
	}
	params := url.Values{"state": {state}, "redirect_uri": {a.cfg.AppURL + "/auth/" + p + "/callback"}}
	endpoint := "https://slack.com/oauth/v2/authorize"
	if p == "github" {
		endpoint = "https://github.com/login/oauth/authorize"
		params.Set("client_id", a.cfg.GitHubClientID)
		params.Set("scope", "user")
		challenge := sha256.Sum256([]byte(verifier))
		params.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		params.Set("code_challenge_method", "S256")
	} else {
		params.Set("client_id", a.cfg.SlackClientID)
		params.Set("user_scope", "users.profile:write")
	}
	http.Redirect(w, r, endpoint+"?"+params.Encode(), http.StatusSeeOther)
}

func (a *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("provider")
	if !a.enabled(p) {
		a.oauthError(w, r, "unavailable")
		return
	}
	s, err := a.session(w, r, false)
	if err != nil || r.URL.Query().Get("state") == "" {
		a.oauthError(w, r, "state")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 35*time.Second)
	defer cancel()
	var verifier string
	// Consuming the pending state atomically makes callbacks single-use.
	err = a.store.db.QueryRowContext(ctx, `UPDATE sessions SET oauth_state='' WHERE id=$1 AND oauth_state=$2 AND oauth_provider=$3 AND oauth_expires>$4 RETURNING oauth_verifier`, s.id, digest(r.URL.Query().Get("state")), p, time.Now().Unix()).Scan(&verifier)
	if err != nil {
		a.oauthError(w, r, "state")
		return
	}
	if r.URL.Query().Get("error") != "" {
		a.oauthError(w, r, "cancelled")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		a.oauthError(w, r, "oauth")
		return
	}
	id, err := a.exchange(ctx, p, code, verifier)
	if err != nil {
		a.oauthError(w, r, "oauth")
		return
	}
	if s.account != "" {
		unlock, err := a.store.lock(ctx, s.account)
		if err != nil {
			a.oauthError(w, r, "busy")
			return
		}
		defer unlock()
	}
	newSession, raw, err := a.linkIdentity(ctx, s, p, id)
	if err != nil {
		if errors.Is(err, errLinked) {
			a.oauthError(w, r, "linked")
		} else {
			a.oauthError(w, r, "oauth")
		}
		return
	}
	_ = newSession
	a.cookie(w, raw, 30*24*3600)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

var errLinked = errors.New("account already linked")

func (a *Server) linkIdentity(ctx context.Context, s session, provider string, id identity) (session, string, error) {
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		return session{}, "", err
	}
	defer tx.Rollback()
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM connections WHERE provider=$1 AND remote_id=$2`, provider, id.remoteID).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return session{}, "", err
	}
	if owner != "" && s.account != "" && owner != s.account {
		return session{}, "", errLinked
	}
	account := s.account
	if account == "" {
		account = owner
	}
	if account == "" {
		account = randomToken()
		if _, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,name) VALUES($1,$2)`, account, id.name); err != nil {
			return session{}, "", err
		}
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT remote_id FROM connections WHERE account_id=$1 AND provider=$2`, account, provider).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return session{}, "", err
	}
	if previous != "" && previous != id.remoteID {
		return session{}, "", errLinked
	}
	encrypted := a.store.encrypt(id.token, provider+":"+id.remoteID)
	_, err = tx.ExecContext(ctx, `INSERT INTO connections(account_id,provider,remote_id,label,token) VALUES($1,$2,$3,$4,$5) ON CONFLICT(account_id,provider) DO UPDATE SET token=excluded.token,label=excluded.label,reconnect=0,error=''`, account, provider, id.remoteID, id.label, encrypted)
	if err != nil {
		return session{}, "", err
	}
	raw := randomToken()
	result := session{id: digest(raw), account: account, csrf: randomToken()}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,account_id,csrf,expires) VALUES($1,$2,$3,$4)`, result.id, account, result.csrf, time.Now().Add(30*24*time.Hour).Unix())
	if err != nil {
		return session{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=$1`, s.id); err != nil {
		return session{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return session{}, "", err
	}
	return result, raw, nil
}
