# I’ll Be Back

[Open the app](https://ill-be-back-production.up.railway.app)

One switch to announce your absence across GitHub and Slack. Press **I’m away**
to set your saved per-account messages; press **I’m back** to clear them. No scheduling,
return dates, or notification changes. Gmail and Google Calendar are the next milestone.

Go backend (`net/http`, `database/sql`), React + TypeScript frontend, SQLite locally,
PostgreSQL on Railway. The same Go binary serves the API and compiled frontend.

## Local development

Requires Go 1.26+ and Node 22+.

```sh
cd frontend
npm ci
npm run build
cd ..
go run ./cmd/server -dev
```

Open http://localhost:8000. Connect an account using configured OAuth credentials.
Development mode generates a local encryption key in `data/token.key` and uses `data/app.db`; keep them together.

For frontend hot reload, run `npm run dev` in `frontend/` while the Go server runs.
Vite proxies `/api` and `/auth` to port 8000. OAuth returns to `APP_URL` (the Go server).

Environment variables are read from the process environment. `.env.example` documents
them; `.env` files are not loaded automatically. For a local OAuth configuration:

```sh
set -a
source .env
set +a
go run ./cmd/server -dev
```

Use separate local OAuth apps with callbacks on `http://localhost:8000`.

## OAuth setup

Connecting the first account also signs the user in. While signed in, use **Add another**
to connect more accounts from the same provider or a different provider. Each connection
has its own message and update result; the main switch updates all of them. Slack identities
include both workspace and user, so multiple users in one workspace are supported.
Any connected identity can sign back into the same account. An external identity belongs
to only one account; accounts are never merged by email. Authorizing the same identity
again refreshes its token without creating a duplicate. Targeted reconnection rejects
a different identity. Disconnecting one connection leaves the others intact.

### GitHub

1. Register an OAuth App at https://github.com/settings/applications/new.
2. Set Homepage URL to `APP_URL`, callback to `APP_URL/auth/github/callback`.
3. Disable **Expire user access tokens** for this release. Token refresh is not implemented.
4. Set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` on the server.

The app requests the `user` scope for profile status updates (no repository scope).
It uses OAuth state, session rotation and GitHub PKCE. Away sets `message`,
`:palm_tree:`, and `limitedAvailability: true`, without an expiry. Back clears the
message and emoji and disables limited availability; it does not restore an old status.

### Slack

1. Sign in at https://api.slack.com/apps and create an app using
   [`deploy/slack-manifest.json`](deploy/slack-manifest.json).
2. Replace the example callback with `APP_URL/auth/slack/callback` if your domain differs.
3. Keep token rotation disabled. This release requires non-rotating user tokens.
4. Set `SLACK_CLIENT_ID` and `SLACK_CLIENT_SECRET` from Basic Information on the server.
5. Enable public distribution to allow installation in other workspaces when launching
   beyond your own workspace. Workspace owners may require approval.

Only the user scope `users.profile:write` is requested. Slack’s own availability dot,
DND, and notification preferences are unchanged. The status text and palm emoji have
no expiry. Back clears both. The app uses the installing user’s token, never a bot token.

## Railway

The Dockerfile builds both applications and runs an unprivileged, static Go binary.
`railway.json` configures the `/healthz` deployment check. No separate worker is needed.

1. Create a project with PostgreSQL and an `ill-be-back` service.
2. Deploy this repository to the service. Dockerfile detection is automatic.
3. Generate a service domain and set:

   | Variable | Value |
   | --- | --- |
   | `APP_URL` | The generated `https://…up.railway.app` origin, without a trailing slash |
   | `PORT` | `8000` |
   | `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` |
   | `TOKEN_ENCRYPTION_KEY` | Output of `openssl rand -base64 32` |
   | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | GitHub OAuth app credentials |
   | `SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET` | Slack app credentials |

4. Update both providers’ redirect URLs to this domain and deploy.

Back up PostgreSQL and the encryption key separately. Changing that key makes stored
provider tokens unreadable and requires reconnection. Production refuses to start
without a database, a valid key, and an HTTPS origin. SQLite deployment is possible
with a writable persistent volume, but PostgreSQL is recommended for Railway.

## Behavior and reliability

- Tokens are encrypted with AES-256-GCM and bound to their provider identity.
- Sessions are opaque random cookies, hashed in the database; cookies are HTTP-only,
  SameSite=Lax and secure in production. Mutations require a session-bound CSRF token
  and reject cross-origin requests.
- Each connected account’s result is persisted separately. Unknown or failed results never count
  as synchronized. Successful updates remain when another integration fails.
- Account-level database leases serialize mutations across tabs and server instances.
  Accepted switches finish within a bounded timeout even if the browser disconnects.
- The dashboard shows the last confirmed write, not a continuous live monitor of changes
  made directly in Slack/GitHub. Pressing the switch writes the requested state again.
- Saving messages changes the next away message; it does not silently change an active
  status. The displayed status retains the message actually applied.
- Disconnect clears the provider status before removing its token. If clearing fails,
  the connection is retained for retry/reconnection. Removing the last connection also
  deletes the account and its sessions. Signing out alone leaves statuses unchanged.

## Checks

```sh
go vet ./...
go test -race ./...
cd frontend
npm run typecheck
npm run build
npx prettier --check src
```

Tests exercise the HTTP API with real sessions and SQLite persistence. External provider
HTTP responses are simulated; the suite never changes a real account’s status. It covers
OAuth replay, account isolation, CSRF, message validation, exact status payloads, partial
failure/reconnection, malformed provider replies, concurrent switches, and disconnection.

The confirmed scope is in [`docs/spec.md`](docs/spec.md). The interface uses a simple
white page, blue accents, and system fonts, inspired by https://playaphone.com/.

Database upgrades run transactionally at startup and preserve existing connections,
sessions, encrypted tokens, and messages. Tests cover upgrading the original schema and
restarting. Set `TEST_POSTGRES_URL` to an **empty disposable database** to also run that
upgrade test against PostgreSQL.
