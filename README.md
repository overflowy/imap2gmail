# imap2gmail

A self-contained mass-migrations orchestrator for Gmail.

<img width="2402" height="1928" alt="app" src="https://github.com/user-attachments/assets/4f0f7145-2837-4e99-8632-d386cfb2f44c" />

Run one binary, open the browser, add dozens of source mailboxes, authorize each destination Gmail once, and sync them in parallel with live per-account logs.

## Why

TL;DR: As a Google Admin, I got frustrated with the Google Import Tool. It sucks. It's slow, completely opaque, and doesn't even work properly.

## Features

```
┌──────────────┐     ┌─────────────────────┐     ┌───────────────────┐
│  origin IMAP │ ──▶ │  imap2gmail (local) │ ──▶ │  Gmail (per-user) │
│  (host:port) │     │  + imapsync workers │     │  via OAuth token  │
└──────────────┘     └─────────────────────┘     └───────────────────┘
                            │
                     ┌──────┴──────┐
                     │  browser UI │  jobs orchestration
                     └─────────────┘
```

- **Bulk import** - paste CSV (`source_user,password,gmail`) to add many accounts at once.
- **Per-account Gmail OAuth** - authorize each destination once; access tokens are refreshed automatically sync.
- **Bounded parallelism** - a worker pool (1–8 concurrent syncs) processes checked accounts.
- **Live logs** - combined imapsync stdout/stderr streamed per operation via SSE, with status indicators (running/stopped).
- **Clean Stop** - Stop / Ctrl-C kills the whole process group (imapsync + forked helpers), persists `stopped` status.
- **Dry Run** - `--dry` is on until you explicitly turn it off.

## Requirements

- **[imapsync](https://imapsync.lamiral.info/)** on `PATH` (the app preflights this on startup). Install e.g. with `brew install imapsync`.
- A **Google OAuth client** (web app type) - you need its Client ID and Secret, and you must add the redirect URI (see [Configuration](#configuration)) to the authorized redirect URIs in Google Cloud Console.

## Build

```sh
# Build the self-contained binary (generates sqlc, builds frontend, embeds it)
task build
```

This produces `imap2gmail`. The frontend is compiled via Vite and embedded into the Go binary via `//go:embed`.

## Run

```sh
./imap2gmail
```

Make sure you're running `imap2gmail` in a secure environment. Treat the environment as you would treat `.env.prod` files.

The server binds `127.0.0.1:<bind_port>` (default `8080`) and opens a browser to it. All state lives under the current working directory:

| Path | Contents | Perms |
|---|---|---|
| `data/db/data.db` | SQLite - settings, accounts, destinations, OAuth tokens | `0600` |
| `data/logs/<source_user>/<timestamp>.log` | per-operation sync logs | `0600` |
| `run/token-<id>.txt` | transient Gmail access-token files passed to imapsync | `0600` |

## Configuration

Open the **Settings** panel in the UI (its collapse state is remembered across reloads):

| Setting | Default | Notes |
|---|---|---|
| OAuth Client ID / Secret | *(empty)* | Google OAuth Desktop credentials |
| Origin Host / Port / SSL | `""` / `993` / on | Your source IMAP server |
| Bind Port | `8080` | Loopback listen port |
| imapsync flags | see below | Global behavior flags (validated) |
| Max Concurrent | `1` | Worker pool, clamped 1–8 |
| Dry Run | **on** | Adds `--dry` until you turn it off |

### Redirect URL

The redirect URL is **derived**, not stored: `http://127.0.0.1:<bind_port>/`. Add it to **Authorized redirect URIs** in your Google OAuth client. Changing `bind_port` requires re-registering the URI and restarting the app (the server only rebinds on restart).

### How Gmail auth works

1. Click **Auth** on an account → the app opens Google's consent URL (requesting offline access + a refresh token).
2. Google redirects to `http://127.0.0.1:<bind_port>/?code=…&state=<nonce>`.
3. The app exchanges the code, stores the refresh token, and marks the destination authenticated.
4. During sync, access tokens are minted from the refresh token and refreshed every 5 minutes. A manual **Exchange Code** fallback is available if the redirect flow can't complete.

## Using it

1. **Configure** the origin IMAP host/port/SSL and your Google OAuth credentials, then Save.
2. **Authorize** each destination Gmail (Auth button per account).
3. **Check** the accounts you want to migrate (Select All / Select None, or per-row).
4. **Sync All** (or Sync per row). The log pane auto-switches to the first syncing account and shows live output.
5. Watch the status badges: **Running** (pulsing), **OK**, **Failed**, **Skipped**, **Stopped**, **Idle**.
6. **Stop** at any time - running accounts are killed and marked `stopped`.

> Dry Run is on by default. Turn it off in Settings to perform a real migration.

## How sync works

The runner is a single global orchestrator (one sync run at a time):

1. **Queue** - checked accounts (skipping duplicates and unauthenticated destinations) enter a worker pool bounded by `max_concurrent`.
2. **Per account** - mint a Gmail access token → write a `0600` token file → build the imapsync argv → spawn imapsync in its own process group.
3. **imapsync argv** - the app owns connection/auth flags: source (`--host1/--port1/--ssl1/--user1/--password1`), destination hardwired to Gmail (`--host2 imap.gmail.com --port2 993 --ssl2 --user2 <gmail> --oauthaccesstoken2 <tokenfile> --gmail2 --nolog`), plus your global behavior flags, plus `--dry` if Dry Run is on.
4. **Stream** - combined stdout/stderr is written to the operation log file and pushed to the browser over SSE.
5. **Status** - each account ends as `ok`, `failed`, `skipped`, or `stopped`.

### Flag safety

The global imapsync flag string is tokenized with `shlex` (never a shell) and validated against a **denylist** of app-managed flags. You cannot override connection, credentials, identity, OAuth, `--dry`, or logging flags (`--log/--nolog/--logfile/--logdir`) - those are owned by the app.

## Stop & shutdown

- **Stop button** cancels the run and sends `SIGKILL` to each child's whole process group (so forked perl/imapsync helpers die and pipes close). In-flight accounts are marked `stopped`; not-yet-started ones stay `idle`.
- **Ctrl-C / SIGTERM** triggers graceful shutdown: stop the runner, wait up to 15s for finalization, then shut down the HTTP server (5s).
- **Startup recovery** clears any stale `running` status left by a crash.

## Tech stack

- **Backend**: Go 1.26.
- **Frontend**: React 19, Mantine 9, TanStack React Query 5, TypeScript, Vite 8 - embedded into the binary at build time.
- **Engine**: [imapsync](https://imapsync.lamiral.info/) invoked as a subprocess.

## Third-party software

This project invokes `imapsync` as an external program. `imapsync` is not part of this
project’s license. See the imapsync project for its own license terms.

## License

This project is licensed under the **PolyForm Noncommercial License 1.0.0**.

You may use, copy, modify, and distribute this software for noncommercial purposes only.

Commercial use is not permitted without separate written permission. This includes selling,
reselling, sublicensing, hosting as a paid service, or including this software in a paid
product or paid service.

For commercial licensing, contact: overflowy@riseup.net
