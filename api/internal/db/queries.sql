-- name: GetSettings :one
SELECT * FROM settings
WHERE id = 1;

-- name: UpsertSettings :exec
INSERT INTO settings (id, client_id, client_secret, bind_port, origin_host, origin_port,
                      origin_ssl, imapsync_flags, max_concurrent, dry_run)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    client_id      = excluded.client_id,
    client_secret  = excluded.client_secret,
    bind_port      = excluded.bind_port,
    origin_host    = excluded.origin_host,
    origin_port    = excluded.origin_port,
    origin_ssl     = excluded.origin_ssl,
    imapsync_flags = excluded.imapsync_flags,
    max_concurrent = excluded.max_concurrent,
    dry_run        = excluded.dry_run;

-- name: UpsertDestination :exec
INSERT INTO destinations (gmail, refresh_token, access_token, access_expiry)
VALUES (?, '', '', '')
ON CONFLICT(gmail) DO NOTHING;

-- name: SetDestinationTokens :exec
UPDATE destinations
SET refresh_token = ?, access_token = ?, access_expiry = ?
WHERE gmail = ?;

-- name: GetDestination :one
SELECT * FROM destinations
WHERE gmail = ?;

-- name: ListDestinations :many
SELECT * FROM destinations
ORDER BY gmail;

-- name: ListAccounts :many
SELECT
    a.id, a.source_user, a.source_password, a.dest_gmail,
    a.sync_checked, a.last_status, a.last_synced_at,
    d.refresh_token, d.access_token, d.access_expiry
FROM accounts a
LEFT JOIN destinations d ON d.gmail = a.dest_gmail
ORDER BY a.id;

-- name: GetAccountsBySource :many
SELECT * FROM accounts
WHERE source_user = ?
ORDER BY id;

-- name: GetAccount :one
SELECT * FROM accounts
WHERE id = ?;

-- name: InsertAccount :one
INSERT INTO accounts (source_user, source_password, dest_gmail, sync_checked, last_status, last_synced_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateAccount :exec
UPDATE accounts
SET source_user     = ?,
    source_password = ?,
    dest_gmail      = ?,
    sync_checked    = ?,
    last_status     = ?,
    last_synced_at  = ?
WHERE id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts
WHERE id = ?;

-- name: SetAccountChecked :exec
UPDATE accounts SET sync_checked = ? WHERE id = ?;

-- name: SetAllChecked :exec
UPDATE accounts SET sync_checked = ?;

-- name: SetAccountStatus :exec
UPDATE accounts SET last_status = ? WHERE id = ?;

-- name: SetAccountSynced :exec
UPDATE accounts SET last_status = ?, last_synced_at = ? WHERE id = ?;

-- name: ResetRunningToIdle :exec
UPDATE accounts SET last_status = 'idle' WHERE last_status = 'running';

-- name: DeleteDestination :exec
DELETE FROM destinations WHERE gmail = ?;

-- name: CreateNonce :exec
INSERT INTO auth_nonces (nonce, dest_gmail, created_at)
VALUES (?, ?, ?);

-- name: GetNonce :one
SELECT * FROM auth_nonces WHERE nonce = ?;

-- name: DeleteNonce :exec
DELETE FROM auth_nonces WHERE nonce = ?;

-- name: SweepNonces :exec
DELETE FROM auth_nonces WHERE created_at < ?;
