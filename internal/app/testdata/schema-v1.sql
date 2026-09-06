
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
)