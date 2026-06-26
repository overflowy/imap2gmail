-- Singleton global configuration (id always = 1).
CREATE TABLE IF NOT EXISTS settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    client_id       TEXT NOT NULL DEFAULT '',
    client_secret   TEXT NOT NULL DEFAULT '',
    -- redirect_url is NOT stored: it is derived at runtime as
    -- http://127.0.0.1:<bind_port>/ (see §1, §5).
    bind_port       INTEGER NOT NULL DEFAULT 8080 CHECK (bind_port BETWEEN 1 AND 65535),
    origin_host     TEXT NOT NULL DEFAULT '',
    origin_port     INTEGER NOT NULL DEFAULT 993,
    origin_ssl      INTEGER NOT NULL DEFAULT 1,         -- bool
    imapsync_flags  TEXT NOT NULL DEFAULT
        '__DEFAULT_IMAPSYNC_FLAGS__',
    max_concurrent  INTEGER NOT NULL DEFAULT 1 CHECK (max_concurrent BETWEEN 1 AND 8),
    dry_run         INTEGER NOT NULL DEFAULT 1          -- bool, default ON for safety
);

-- One Gmail destination; owns OAuth tokens, shared by many Accounts.
CREATE TABLE IF NOT EXISTS destinations (
    gmail          TEXT PRIMARY KEY,
    refresh_token  TEXT NOT NULL DEFAULT '',
    access_token   TEXT NOT NULL DEFAULT '',
    access_expiry  TEXT NOT NULL DEFAULT ''             -- RFC3339, last minted token
);

-- One source mailbox -> one destination.
-- source_user SHOULD be unique but is intentionally NOT constrained UNIQUE:
-- duplicates are allowed to exist so the UI can flag them red; they are blocked
-- from syncing. A plain (non-unique) index supports import upsert + dup detection.
CREATE TABLE IF NOT EXISTS accounts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    source_user     TEXT NOT NULL,
    source_password TEXT NOT NULL DEFAULT '',
    dest_gmail      TEXT NOT NULL REFERENCES destinations(gmail) ON DELETE CASCADE,
    sync_checked    INTEGER NOT NULL DEFAULT 1,         -- bool, persisted check state
    last_status     TEXT NOT NULL DEFAULT 'idle',       -- idle|running|ok|failed|skipped|stopped
    last_synced_at  TEXT NOT NULL DEFAULT ''            -- RFC3339
);
CREATE INDEX IF NOT EXISTS idx_accounts_source ON accounts(source_user);

-- Short-lived nonces correlating an OAuth callback to a destination.
CREATE TABLE IF NOT EXISTS auth_nonces (
    nonce       TEXT PRIMARY KEY,
    dest_gmail  TEXT NOT NULL,
    created_at  TEXT NOT NULL                            -- RFC3339, used for TTL sweep
);
