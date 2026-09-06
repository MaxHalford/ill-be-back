package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

func (a *Server) changeStatus(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authenticated(w, r)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Status != "away" && input.Status != "available" {
		problem(w, 400, "Choose away or available.")
		return
	}
	// Complete accepted changes even if the browser closes before the providers respond.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 35*time.Second)
	defer cancel()
	unlock, err := a.store.lock(ctx, s.account)
	if err != nil {
		a.lockError(w, err)
		return
	}
	defer unlock()
	connections, err := a.store.connections(ctx, s.account)
	if err != nil {
		internalError(w, err)
		return
	}
	if len(connections) == 0 {
		problem(w, 400, "Connect an app first.")
		return
	}
	_, err = a.store.db.ExecContext(ctx, `UPDATE accounts SET desired_status=$1 WHERE id=$2`, input.Status, s.account)
	if err != nil {
		internalError(w, err)
		return
	}
	for _, c := range connections {
		if _, err = a.applyStatus(ctx, s.account, c, input.Status); err != nil {
			internalError(w, err)
			return
		}
	}
	a.respondState(w, r.WithContext(ctx), s)
}

// Each provider result is durable independently. A timeout has an unknown outcome,
// and is never represented as either a successful change or a safe rollback.
func (a *Server) applyStatus(ctx context.Context, account string, c Connection, target string) (bool, error) {
	_, err := a.store.db.ExecContext(ctx, `UPDATE connections SET status='unknown',error='Update not yet confirmed.' WHERE account_id=$1 AND provider=$2`, account, c.Provider)
	if err != nil {
		return false, err
	}
	if err = a.setStatus(ctx, c, target); err != nil {
		message, reconnect := "We couldn’t confirm the update. Please try again.", 0
		var failure *providerError
		if errors.As(err, &failure) {
			message = failure.message
			if failure.reconnect {
				reconnect = 1
			}
		}
		_, err = a.store.db.ExecContext(ctx, `UPDATE connections SET error=$1,reconnect=$2 WHERE account_id=$3 AND provider=$4`, message, reconnect, account, c.Provider)
		return false, err
	}
	_, err = a.store.db.ExecContext(ctx, `UPDATE connections SET status=$1,error='',reconnect=0,updated_at=$2,applied_message=$3 WHERE account_id=$4 AND provider=$5`, target, time.Now().UTC().Format(time.RFC3339), c.Message, account, c.Provider)
	return err == nil, err
}

func (a *Server) lockError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBusy) {
		problem(w, 409, "Another status update is in progress. Please try again in a moment.")
	} else {
		internalError(w, err)
	}
}

func (a *Server) messages(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authenticated(w, r)
	if !ok {
		return
	}
	var input struct {
		Messages map[string]string `json:"messages"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Messages) == 0 {
		problem(w, 400, "Add a message to save.")
		return
	}
	for p, message := range input.Messages {
		limit := 100
		if p == "github" {
			limit = 80
		} else if p != "slack" {
			problem(w, 400, "Unknown app.")
			return
		}
		message = strings.TrimSpace(message)
		if message == "" || !utf8.ValidString(message) || utf8.RuneCountInString(message) > limit {
			problem(w, 400, "Use a non-empty message within the app’s character limit.")
			return
		}
		input.Messages[p] = message
	}
	unlock, err := a.store.lock(r.Context(), s.account)
	if err != nil {
		a.lockError(w, err)
		return
	}
	defer unlock()
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	for p, message := range input.Messages {
		if _, err = tx.ExecContext(r.Context(), `UPDATE connections SET message=$1 WHERE account_id=$2 AND provider=$3`, message, s.account, p); err != nil {
			internalError(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	a.respondState(w, r, s)
}

func (a *Server) logout(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authenticated(w, r)
	if !ok {
		return
	}
	if _, err := a.store.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id=$1`, s.id); err != nil {
		internalError(w, err)
		return
	}
	a.cookie(w, "", -1)
	a.respondState(w, r, session{})
}

func (a *Server) disconnect(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authenticated(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 25*time.Second)
	defer cancel()
	unlock, err := a.store.lock(ctx, s.account)
	if err != nil {
		a.lockError(w, err)
		return
	}
	defer unlock()
	connections, err := a.store.connections(ctx, s.account)
	if err != nil {
		internalError(w, err)
		return
	}
	var found *Connection
	for _, c := range connections {
		if c.Provider == r.PathValue("provider") {
			found = &c
			break
		}
	}
	if found == nil {
		problem(w, 404, "This app isn’t connected.")
		return
	}
	confirmed, err := a.applyStatus(ctx, s.account, *found, "available")
	if err != nil {
		internalError(w, err)
		return
	}
	if !confirmed {
		problem(w, 502, "We couldn’t clear this app’s status. Reconnect it or try again before disconnecting.")
		return
	}
	if len(connections) == 1 {
		_, err = a.store.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, s.account)
		if err != nil {
			internalError(w, err)
			return
		}
		a.cookie(w, "", -1)
		a.respondState(w, r.WithContext(ctx), session{})
		return
	}
	_, err = a.store.db.ExecContext(ctx, `DELETE FROM connections WHERE account_id=$1 AND provider=$2`, s.account, found.Provider)
	if err != nil {
		internalError(w, err)
		return
	}
	a.respondState(w, r.WithContext(ctx), s)
}
