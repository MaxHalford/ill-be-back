package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Connection struct {
	Provider       string  `json:"provider"`
	Label          string  `json:"label"`
	Message        string  `json:"message"`
	AppliedMessage string  `json:"appliedMessage"`
	Status         string  `json:"status"`
	Error          string  `json:"error"`
	NeedsReconnect bool    `json:"needsReconnect"`
	UpdatedAt      *string `json:"updatedAt"`
	remoteID       string
	token          string
}

type store struct {
	db   *sql.DB
	aead cipher.AEAD
}

func openStore(dsn string, key []byte) (*store, error) {
	driver := "sqlite"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		driver = "pgx"
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	block, err := aes.NewCipher(key)
	if err != nil {
		db.Close()
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &store{db: db, aead: aead}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if driver == "sqlite" {
		if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=10000;"); err != nil {
			db.Close()
			return nil, err
		}
	}
	// Additive, idempotent initial schema: works on SQLite and PostgreSQL.
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err = db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, desired_status TEXT NOT NULL DEFAULT 'available',
 lock_until BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
 id TEXT PRIMARY KEY, account_id TEXT REFERENCES accounts(id) ON DELETE CASCADE,
 csrf TEXT NOT NULL, expires BIGINT NOT NULL,
 oauth_state TEXT NOT NULL DEFAULT '', oauth_provider TEXT NOT NULL DEFAULT '',
 oauth_verifier TEXT NOT NULL DEFAULT '', oauth_expires BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires);
CREATE TABLE IF NOT EXISTS connections (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 provider TEXT NOT NULL, remote_id TEXT NOT NULL, label TEXT NOT NULL, token TEXT NOT NULL,
 message TEXT NOT NULL DEFAULT 'Out of office', applied_message TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'unknown',
 error TEXT NOT NULL DEFAULT '', reconnect INTEGER NOT NULL DEFAULT 0, updated_at TEXT,
 PRIMARY KEY(account_id, provider), UNIQUE(provider, remote_id)
)`

func randomToken() string { return rand.Text() }
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *store) encrypt(token, identity string) string {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(token), []byte(identity)))
}

func (s *store) decrypt(token, identity string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(data) < s.aead.NonceSize() {
		return "", errors.New("invalid token ciphertext")
	}
	plain, err := s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], []byte(identity))
	return string(plain), err
}

func (s *store) connections(ctx context.Context, account string) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider,label,message,applied_message,status,error,reconnect,updated_at,remote_id,token FROM connections WHERE account_id=$1 ORDER BY provider`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Connection{}
	for rows.Next() {
		var c Connection
		var reconnect int
		if err := rows.Scan(&c.Provider, &c.Label, &c.Message, &c.AppliedMessage, &c.Status, &c.Error, &reconnect, &c.UpdatedAt, &c.remoteID, &c.token); err != nil {
			return nil, err
		}
		c.NeedsReconnect = reconnect != 0
		result = append(result, c)
	}
	return result, rows.Err()
}

var errBusy = errors.New("another update is in progress")

func (s *store) lock(ctx context.Context, account string) (func(), error) {
	until := time.Now().Add(2 * time.Minute).UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE accounts SET lock_until=$1 WHERE id=$2 AND lock_until<$3`, until, account, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, errBusy
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.db.ExecContext(ctx, `UPDATE accounts SET lock_until=0 WHERE id=$1 AND lock_until=$2`, account, until); err != nil {
			// The lease expires even if the database is temporarily unavailable.
			fmt.Println("could not release account update lease")
		}
	}, nil
}
