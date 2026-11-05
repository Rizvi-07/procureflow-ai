# Accounts, signup and admin roles

Users are stored in PostgreSQL in **auth.users**. The table contains the stable
user ID, unique normalized username, display name, Argon2id password hash, role,
session version and UTC timestamps. Passwords are never stored in plaintext in
SQL. Signup accounts are not written to local credential files.

The gateway identity module owns auth.*. It does not access purchase.* or
approval.*; procurement still goes through gRPC. See [architecture.md](architecture.md).

## Use the application

Open **http://localhost:5173/**. Choose **Create an account**, enter a fictional
display name, username and password, then sign in. Every signup starts as an
**employee**. No employee ID or privileged role can be supplied during signup.

- Employee: create purchases and view only their own.
- Manager: view all purchases and review other people's requests.
- Admin: manager permissions plus user listing and role assignment. Open
  **Manage users** in the account bar, choose a role and select **Save role**.

Managers and admins cannot approve or reject their own purchases. Admins can
assign employee, manager or admin, but the last admin cannot be demoted.
Role changes invalidate the affected user's existing sessions on their next
HTTP request; they must sign in again to use the new role. An identical role
retry is safe and does not invalidate sessions again.

Username: 3-80 ASCII letters/digits/dots/underscores/hyphens, starting with a
letter or digit. Whitespace is trimmed and case normalized before uniqueness
checks. Display name: 1-120 characters. Password: 12-128 UTF-8 bytes. Signup and
login are rate limited and use bounded password-hashing concurrency. Errors,
loading states and password confirmation are shown in the UI.

## Upgrade the existing local installation

The existing demo passwords, employee IDs, orders and signing key are preserved.
Run from the repository root:

```powershell
# Once only, if the admin files do not exist yet:
go run ./cmd/auth-setup -admin-only
# Skip setup on later runs. Never overwrite credential files.
docker compose up -d --build --wait
docker compose ps -a
```

Sign in as **admin.demo**, using the password in the ignored
**secrets/admin-credentials.json** file. Existing employee.demo, employee.two and
manager.demo passwords remain in secrets/demo-credentials.json. These are
initial local credentials; SQL is now the source of truth for accounts/roles.
Editing an old JSON file does not change an existing SQL account.

Compose runs migrations, then the one-shot **auth-seed** job, before starting
the gateway. auth-seed imports the legacy hash file and initial admin hash file
in one transaction. Reruns insert missing accounts and preserve existing SQL
passwords and admin-assigned roles; ID/username conflicts fail rather than
silently granting access. Both migrate and auth-seed exiting with code 0 is
normal. Database data and users survive container recreation via the named
volume. Never delete that volume for routine shutdown.

## Fresh installation

```powershell
go run ./cmd/auth-setup
docker compose up -d --build --wait
```

Setup refuses to overwrite any existing credential file. It generates:

- secrets/auth-users.json: legacy employee/manager bootstrap hashes and IDs.
- secrets/admin-account.json: the initial admin hash and identity.
- secrets/demo-credentials.json and secrets/admin-credentials.json: initial
  generated passwords, for local viewing only.
- secrets/service-auth.key: the backend signing key.

All five files are ignored by Git and excluded from image build contexts.
Only auth-seed mounts bootstrap hashes. The gateway/services mount the signing
key; runtime login reads SQL, not files. New signup passwords exist only as
hashes in SQL. No universal default password or first-signup admin rule exists.

Without host Go, provision files using Docker instead:

```powershell
New-Item -ItemType Directory -Force secrets | Out-Null
docker build --target auth-setup -t procureflow-auth-setup .
docker run --rm --mount "type=bind,source=$((Get-Location).Path)\secrets,target=/workspace/secrets" procureflow-auth-setup
# For an existing installation, append -admin-only to the docker run command.
docker compose up -d --build --wait
```

## Native development

Provision files first, then set local URLs in the shell (replace placeholders):

```powershell
$env:AUTH_DATABASE_URL = 'postgres://USER:PASSWORD@localhost:5432/procureflow_dev?sslmode=disable'
$env:DATABASE_URL = $env:AUTH_DATABASE_URL
go run ./cmd/migrate
go run ./cmd/auth-seed
# Then start the two gRPC services and gateway in separate terminals.
```

The gateway requires AUTH_DATABASE_URL and migrated/seeded auth tables; it has
no file or memory fallback in runtime. DATABASE_URL remains the procurement
services' separate setting. Unit tests use a concurrent memory fake behind the
same Users interface. Context-aware pgxpool queries have deadlines and close
with the gateway. Migrations are versioned under the existing active migrations/
directory; 003_auth.sql adds auth.users and auth.role_changes without editing
applied migrations or adding cross-schema foreign keys. TEXT user IDs preserve
legacy emp-101 ownership; signup IDs are generated UUID strings.

Native Go does not load .env automatically. SERVICE_AUTH_KEY_FILE defaults to
secrets/service-auth.key. auth-seed accepts -users-file and -admin-file path
options. AUTH_USERS_FILE is no longer used by the runtime gateway.

Use localhost for both Vue and the browser API address. Mixing localhost and
127.0.0.1 breaks the SameSite cookie flow. Never put credentials in VITE_ vars.
AUTH_COOKIE_SECURE=false is local HTTP only; HTTPS requires true.

## HTTP and security boundaries

- POST /api/auth/signup: username/name/password JSON; 201 public user. Sign in
  separately after signup. Duplicate usernames return 409; role/ID fields are
  rejected as unknown fields, and invalid inputs return 400.
- POST /api/auth/login: username/password JSON; public user, expiry and CSRF
  token, plus an HttpOnly SameSite=Strict session cookie scoped to /api.
- GET /api/auth/me: restore the public identity and CSRF token after a page reload.
- POST /api/auth/logout: revoke the session and clear the cookie.
- GET /api/admin/users: admin only, public fields only, never password hashes.
- POST /api/admin/users/{id}/role: admin only, JSON {"role":"manager"}.
  Accepts exactly employee/manager/admin. Unknown user returns 404; last-admin
  demotion returns 409. All authenticated POSTs require X-CSRF-Token.

Only signup/login, health checks and preflights are public. Exact-origin CORS,
JSON content-type/unknown-field rejection and body limits also apply to signup.
Sessions expire after eight hours. The gateway stores only session-token
SHA-256 digests, and the browser keeps its CSRF token only in memory.

Every authenticated HTTP request reloads the user's role/version from SQL.
Database failures return controlled 503/504 errors; they never fall back to stale
role data. Role changes recheck the acting admin inside a transaction, serialize
with an advisory lock to protect the last admin, increment the target version
and insert auth.role_changes audit metadata atomically. This audit records role
changes; manager IDs/comments on procurement decisions are still not persisted
by the existing approval RPC.

The gateway signs 60-second actor claims in private gRPC metadata after session
verification; services validate signature/expiry and permissions. Issued internal
claims can remain valid until expiry; there is no per-RPC SQL revocation lookup.
The signing key is a trusted backend credential: holders can impersonate actors.
The Protocol Buffer contracts are unchanged. Internal service claims still
restrict approval creation/status propagation to the owning services. The policy
assistant stays read-only and has no purchase actions.

## Verification and limitations

```powershell
go test ./internal/auth ./internal/gateway ./internal/rpc
# TEST_DATABASE_URL enables the six isolated identity/procurement DB tests:
go test -v ./internal/postgres
powershell -NoProfile -File scripts/smoke.ps1
powershell -NoProfile -File scripts/auth-smoke.ps1
# Optional: also restart the gateway and verify SQL-backed signup/role persistence:
powershell -NoProfile -File scripts/auth-smoke.ps1 -RestartGateway
```

The auth smoke test creates a unique fictional user, tests forbidden escalation,
admin role grants, revocation and an actual delegated approval. It leaves its
synthetic user as an employee and retains synthetic purchases. It never resets
existing data. Pass -AdminCredentialsFile for a different bootstrap login file.
Frontend checks remain npm test, npm run build and the opt-in npm run test:live.
Live Vue checks include the signup form and actual admin role editor, with
ADMIN_CREDENTIALS_FILE for a custom admin login file. Their Node transport
emulates a browser cookie jar; they are not a full-browser accessibility check.
Unit and DB tests also cover concurrent last-admin demotions and migration reruns.
No paid AI calls run during these checks.

Sessions and rate limits remain in one gateway process's memory; gateway restart
signs everyone out while SQL accounts/roles/orders persist. Multiple replicas
would need shared sessions and rate limiting. Keep bootstrap files and the named
volume private. Unix setup uses a private parent directory and mode 0600 for
plaintext credential files; Windows access follows your OS account permissions.

This is a loopback-only portfolio app without HTTPS, email verification,
password reset/change, account deletion/disable, MFA or external identity
federation. Those features and least-privilege SQL roles/managed secrets remain
production work. Existing OWASP password/session/CSRF controls remain in place;
this implementation does not claim independent security certification.
