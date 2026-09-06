package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
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
	// Mark every target uncertain atomically before sending requests. If the total
	// deadline expires, queued accounts cannot retain a stale success from an earlier switch.
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET desired_status=$1 WHERE id=$2`, input.Status, s.account); err != nil {
		internalError(w, err)
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE connections SET status='unknown',error='Update not yet confirmed.' WHERE account_id=$1`, s.account); err != nil {
		internalError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	// A few workers prevent additional accounts from turning the switch into a
	// sequence of provider timeouts, without sending unbounded concurrent requests.
	jobs := make(chan Connection, len(connections))
	results := make(chan error, len(connections))
	for _, c := range connections {
		jobs <- c
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(connections)) {
		workers.Go(func() {
			for c := range jobs {
				_, err := a.applyStatus(ctx, s.account, c, input.Status)
				results <- err
			}
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			internalError(w, err)
			return
		}
	}
	a.respondState(w, r.WithContext(ctx), s)
}

// Each provider result is durable independently. A timeout has an unknown outcome,
// and is never represented as either a successful change or a safe rollback.
func (a *Server) applyStatus(ctx context.Context, account string, c Connection, target string) (bool, error) {
	_, err := a.store.db.ExecContext(ctx, `UPDATE connections SET status='unknown',error='Update not yet confirmed.' WHERE account_id=$1 AND id=$2`, account, c.ID)
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
		_, err = a.store.db.ExecContext(ctx, `UPDATE connections SET error=$1,reconnect=$2 WHERE account_id=$3 AND id=$4`, message, reconnect, account, c.ID)
		return false, err
	}
	_, err = a.store.db.ExecContext(ctx, `UPDATE connections SET status=$1,error='',reconnect=0,updated_at=$2,applied_message=$3 WHERE account_id=$4 AND id=$5`, target, time.Now().UTC().Format(time.RFC3339), c.Message, account, c.ID)
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
	unlock, err := a.store.lock(r.Context(), s.account)
	if err != nil {
		a.lockError(w, err)
		return
	}
	defer unlock()
	connections, err := a.store.connections(r.Context(), s.account)
	if err != nil {
		internalError(w, err)
		return
	}
	messages := make(map[string]string, len(input.Messages))
	for key, message := range input.Messages {
		c, ok := resolveConnection(w, connections, key)
		if !ok {
			return
		}
		limit := 100
		if c.Provider == "github" {
			limit = 80
		}
		message = strings.TrimSpace(message)
		if message == "" || !utf8.ValidString(message) || utf8.RuneCountInString(message) > limit {
			problem(w, 400, "Use a non-empty message within the app’s character limit.")
			return
		}
		if _, exists := messages[c.ID]; exists {
			problem(w, 400, "Send one message per account.")
			return
		}
		messages[c.ID] = message
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	for id, message := range messages {
		if _, err = tx.ExecContext(r.Context(), `UPDATE connections SET message=$1 WHERE account_id=$2 AND id=$3`, message, s.account, id); err != nil {
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
	found, ok := resolveConnection(w, connections, r.PathValue("connection"))
	if !ok {
		return
	}
	confirmed, err := a.applyStatus(ctx, s.account, found, "available")
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
	_, err = a.store.db.ExecContext(ctx, `DELETE FROM connections WHERE account_id=$1 AND id=$2`, s.account, found.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	a.respondState(w, r.WithContext(ctx), s)
}

// Accept an old provider-only URL only when it identifies a single connection.
// This allows already-open browser tabs to survive the API upgrade safely.
func resolveConnection(w http.ResponseWriter, connections []Connection, key string) (Connection, bool) {
	for _, c := range connections {
		if c.ID == key {
			return c, true
		}
	}
	var matches []Connection
	for _, c := range connections {
		if c.Provider == key {
			matches = append(matches, c)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	if len(matches) > 1 {
		problem(w, 409, "You have multiple accounts for this app. Refresh the page and choose an account.")
	} else {
		problem(w, 404, "This account isn’t connected.")
	}
	return Connection{}, false
}
