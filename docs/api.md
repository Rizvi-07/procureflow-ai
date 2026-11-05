# REST/JSON API gateway (Phase 04)

Phase 08 adds the independent optional policy endpoint below. Purchase/decision payloads remain compatible, but all API calls now require local authentication. See [authentication.md](authentication.md) to generate local accounts. The health route stays public.

## Optional policy question (Phase 08)

`POST /api/assistant/ask` accepts exactly `{"question":"What is the fictional meal policy?"}`.
Questions must contain 1–1000 UTF-8 bytes without NUL. Standard JSON field/body
validation, CORS restrictions, and controlled gRPC-to-HTTP error mapping apply.
There is no order ID, manager identity, decision, or action input on this route.

Use the authenticated PowerShell example below for this route.

The response is `{"configured":false,"answer":"Not configured...","sources":[]}`
when disabled or unconfigured. A configured answer returns `configured: true`
and source objects with `document`, `section`, `chunk_index`, and exact `quote`.
Empty evidence returns a no-supporting-policy answer with no sources. Invalid
citations/provider failures return controlled errors rather than fabricated
answers. Live OpenAI integration is unverified without credentials/models.
See [policy-assistant.md](policy-assistant.md) for explicit opt-in and costs.

## Authentication

- `POST /api/auth/signup`: `{"username":"employee.new","name":"Fictional Employee","password":"<chosen 12-128 byte password>"}`. Returns 201 with a public employee user; sign in separately. Supplied IDs/roles are rejected. Duplicate usernames return 409.
- `POST /api/auth/login`: `{"username":"employee.demo","password":"<generated password>"}`.
  Returns public `user`, `csrf_token`, `expires_at` and an HttpOnly session cookie.
- `GET /api/auth/me`: requires the cookie; returns public identity and CSRF token.
- `POST /api/auth/logout`: requires cookie and `X-CSRF-Token`; revokes the session.

Only signup and login are public `/api/*` routes. All others require a session; authenticated POSTs also require
`X-CSRF-Token`. Missing/expired sessions return 401, invalid CSRF or forbidden
roles return 403, and throttled login attempts return 429. Employees see only
their own purchases; managers/admins see all but cannot decide their own requests.
`employee_id` must match the signed-in account. `manager_id` is optional but, if
provided, must match the signed-in manager. It cannot confer the manager role.
Manager IDs/comments are not persisted by the status-only approval contract;
responses report `metadata_persisted:false` and `demo_only:false`. There is no
chatbot approval route.

## Admin role assignment

- GET /api/admin/users: admin session required; returns {"users":[...]} with
  id, username, name and role. Password hashes/session versions are not exposed.
- POST /api/admin/users/{id}/role: admin session and X-CSRF-Token required.
  Body: {"role":"manager"}; exactly employee, manager or admin is accepted.
  Returns {"user":{...}}. Unknown user: 404. Last-admin demotion: 409.

Changing a role increments a SQL session version and invalidates that user's
old sessions. Repeating the identical role is safe. Each actual change records
actor/target/old role/new role/time in auth.role_changes atomically. These are
identity audit records, separate from procurement decision metadata. Admins
inherit manager purchase permissions and cannot self-approve/reject.

After signup, an admin can assign a role using PowerShell:

```powershell
$base = 'http://localhost:8080'
$admin = @(Get-Content secrets/admin-credentials.json -Raw | ConvertFrom-Json)[0]
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$login = Invoke-RestMethod -Method Post -Uri "$base/api/auth/login" -WebSession $session -ContentType application/json -Body (@{username=$admin.username;password=$admin.password}|ConvertTo-Json)
$users = Invoke-RestMethod -Uri "$base/api/admin/users" -WebSession $session
$target = $users.users | Where-Object username -eq 'employee.new'
if ($null -eq $target) { throw 'Sign up the fictional employee.new account first' }
Invoke-RestMethod -Method Post -Uri "$base/api/admin/users/$($target.id)/role" -WebSession $session -Headers @{'X-CSRF-Token'=$login.csrf_token} -ContentType application/json -Body '{"role":"manager"}'
```

The browser provides the same actions under Manage users. New signups cannot
select a privileged role; see [authentication.md](authentication.md) for setup.

## Start the gateway

Run commands from the repository root with Go 1.26 or newer. Start the purchase and approval services as described in [backend.md](backend.md); persistent mode is covered in [postgresql.md](postgresql.md). Then open another terminal:

```powershell
go run ./cmd/api-gateway
```

The gateway listens on `http://127.0.0.1:8080`. It uses reusable gRPC connections to `localhost:50051` and `localhost:50052`; it ignores procurement `DATABASE_URL` and accesses only auth.* through `AUTH_DATABASE_URL`. Account migrations and auth-seed must complete before startup. New service processes bind to loopback by default. The browser's only REST/JSON endpoint is the gateway. There is no gRPC-Web endpoint or service-side CORS support.

The gateway can start before its peers. `/healthz` reports gateway process liveness only; API calls return controlled errors while a peer is unavailable. Ctrl+C drains HTTP requests for up to `SHUTDOWN_TIMEOUT` before forcing connections closed.

## Endpoints and JSON

- `GET /healthz` -> 200 with `{"status":"ok","service":"api-gateway"}`.
- `GET /api/purchases` -> 200 with `{"purchases":[...]}`. An empty list is `[]`, never `null`. No pagination is implemented.
- `GET /api/purchases/{id}` -> 200 with a purchase, or 404 if missing.
- `POST /api/purchases` -> 201 with a purchase and its relative `Location` header.
- `POST /api/purchases/{id}/decision` -> 200 with the saved approval status and metadata indicators.

A purchase response includes `id`, `client_request_id`, `employee_id`, `item_name`, `quantity`, `unit_price_cents`, `total_cents`, and `status`. Status strings are `PENDING`, `APPROVED`, or `REJECTED`. Quantities and monetary cents are JSON integer numbers. Go preserves signed 64-bit values; JavaScript clients must account for its smaller safe-integer range when handling unusually large values.

Create input:

```json
{
  "client_request_id": "7e81c280-2204-47bb-89c8-8d777a25a730",
  "employee_id": "emp-101",
  "item_name": "Laptop",
  "quantity": 2,
  "unit_price_cents": 80000
}
```

Both numeric fields are required; zero price is allowed. Quantity must be positive, price nonnegative, and the total must fit int64. IDs use the existing 1-128 character alphanumeric/underscore/hyphen rule and start alphanumeric. Item names contain 1-200 Unicode characters, cannot be blank, and cannot contain NUL. The services remain authoritative for business rules and calculations.

Decision input (`comment` is optional, up to 1000 characters without NUL):

```json
{"manager_id":"manager-demo","decision":"APPROVED","comment":"Within budget"}
```

Only exact `APPROVED` or `REJECTED` decisions are accepted. Example response:

```json
{"purchase_id":"<purchase UUID>","status":"APPROVED","demo_only":false,"metadata_persisted":false}
```

The existing `DecideApproval` contract carries only purchase ID and status. Manager ID/comment are neither audit evidence nor persisted metadata; they are not logged. Protocol Buffer messages are unchanged; signed gRPC metadata carries identity and the Approval Service enforces manager ownership rules.

## PowerShell examples

Run from the repository root after account setup and service startup. Passwords
are loaded from ignored local files and are not printed. Use separate cookie
sessions for employee and manager identities.

```powershell
$base = 'http://localhost:8080'
$credentials = Get-Content secrets/demo-credentials.json -Raw | ConvertFrom-Json
$employee = $credentials | Where-Object username -eq 'employee.demo'
$manager = $credentials | Where-Object username -eq 'manager.demo'
$employeeSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$managerSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$employeeLogin = Invoke-RestMethod -Uri "$base/api/auth/login" -Method Post -WebSession $employeeSession -ContentType application/json -Body (@{username=$employee.username;password=$employee.password}|ConvertTo-Json)
$managerLogin = Invoke-RestMethod -Uri "$base/api/auth/login" -Method Post -WebSession $managerSession -ContentType application/json -Body (@{username=$manager.username;password=$manager.password}|ConvertTo-Json)
$employeeHttp = @{WebSession=$employeeSession;Headers=@{'X-CSRF-Token'=$employeeLogin.csrf_token};TimeoutSec=10}
$managerHttp = @{WebSession=$managerSession;Headers=@{'X-CSRF-Token'=$managerLogin.csrf_token};TimeoutSec=10}
Invoke-RestMethod "$base/healthz"
Invoke-RestMethod @employeeHttp "$base/api/auth/me"
Invoke-RestMethod @employeeHttp "$base/api/purchases"
$body = @{client_request_id=[guid]::NewGuid().ToString();employee_id=$employeeLogin.user.id;item_name='Fictional laptop';quantity=2;unit_price_cents=80000} | ConvertTo-Json
$purchase = Invoke-RestMethod @employeeHttp -Uri "$base/api/purchases" -Method Post -ContentType application/json -Body $body
Invoke-RestMethod @employeeHttp "$base/api/purchases/$($purchase.id)"
$decision = @{manager_id=$managerLogin.user.id;decision='APPROVED';comment='Fictional local demo'} | ConvertTo-Json
Invoke-RestMethod @managerHttp -Uri "$base/api/purchases/$($purchase.id)/decision" -Method Post -ContentType application/json -Body $decision
Invoke-RestMethod @employeeHttp "$base/api/purchases/$($purchase.id)"
# Optional read-only assistant, including Not configured when disabled:
Invoke-RestMethod @employeeHttp -Method Post -Uri "$base/api/assistant/ask" -ContentType application/json -Body '{"question":"What is the fictional meal limit?"}'
Invoke-RestMethod @employeeHttp -Method Post -Uri "$base/api/auth/logout" -ContentType application/json -Body '{}'
Invoke-RestMethod @managerHttp -Method Post -Uri "$base/api/auth/logout" -ContentType application/json -Body '{}'
```

For repeatable checks with assertions, run `powershell -NoProfile -File scripts/smoke.ps1`.

## curl examples

POSIX shell with `jq` and `uuidgen`, from the repository root. Local cookie/session files stay
under ignored `tmp/`; protect them like credentials. Windows users should use
the PowerShell example above. Account passwords are piped through stdin rather
than copied into command arguments or shell history.

```sh
mkdir -p tmp
chmod 700 tmp
base=http://localhost:8080
jq -c '.[] | select(.username=="employee.demo") | {username,password}' secrets/demo-credentials.json |
  curl -fsS -c tmp/employee.cookies "$base/api/auth/login" -H 'Content-Type: application/json' --data-binary @- > tmp/employee-session.json
jq -c '.[] | select(.username=="manager.demo") | {username,password}' secrets/demo-credentials.json |
  curl -fsS -c tmp/manager.cookies "$base/api/auth/login" -H 'Content-Type: application/json' --data-binary @- > tmp/manager-session.json
employee_csrf=$(jq -r .csrf_token tmp/employee-session.json)
manager_csrf=$(jq -r .csrf_token tmp/manager-session.json)
curl -fsS "$base/healthz"
curl -fsS -b tmp/employee.cookies "$base/api/purchases"
request_id=$(uuidgen)
curl -fsS -b tmp/employee.cookies "$base/api/purchases" -H 'Content-Type: application/json' -H "X-CSRF-Token: $employee_csrf" \
  -d "{\"client_request_id\":\"$request_id\",\"employee_id\":\"emp-101\",\"item_name\":\"Fictional laptop\",\"quantity\":2,\"unit_price_cents\":80000}" > tmp/purchase.json
purchase_id=$(jq -r .id tmp/purchase.json)
curl -fsS -b tmp/employee.cookies "$base/api/purchases/$purchase_id"
curl -fsS -b tmp/manager.cookies "$base/api/purchases/$purchase_id/decision" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $manager_csrf" -d '{"decision":"APPROVED","comment":"Fictional demo"}'
curl -fsS -b tmp/employee.cookies "$base/api/purchases/$purchase_id"
curl -fsS -b tmp/employee.cookies "$base/api/auth/logout" -H 'Content-Type: application/json' -H "X-CSRF-Token: $employee_csrf" -d '{}'
curl -fsS -b tmp/manager.cookies "$base/api/auth/logout" -H 'Content-Type: application/json' -H "X-CSRF-Token: $manager_csrf" -d '{}'
```

## JSON validation and errors

POST requires `Content-Type: application/json` (a charset parameter is allowed). Bodies must be valid UTF-8 and contain exactly one non-null object. Unknown fields, malformed JSON, wrong types, fractions for integer fields, missing required values, invalid IDs, and invalid decisions return 400. Oversized bodies return 413; unsupported media types or content encoding return 415. Unknown routes return JSON 404, and unsupported methods return JSON 405 with an `Allow` header.

Every application error uses the same envelope:

```json
{"error":{"code":"UNAVAILABLE","message":"backend unavailable; retry with the same create key and details or the same decision","retryable":true}}
```

Backend status mapping:

- `InvalidArgument` / `OutOfRange` -> 400.
- `NotFound` -> 404.
- `AlreadyExists` / `FailedPrecondition` / `Aborted` -> 409.
- `Unauthenticated` -> 401; `PermissionDenied` -> 403.
- `ResourceExhausted` -> 429; `Unavailable` -> 503; `DeadlineExceeded` -> 504.
- `Canceled` -> 408 if the client is still connected.
- Internal, unknown, unimplemented, data-loss, or malformed peer responses -> controlled 502; an unexpected gateway panic -> 500.

Peer error messages are not exposed to clients or request logs. Local validation errors describe the invalid field. Retryable errors include `Retry-After: 1`; the gateway never retries mutations automatically.

Repeat purchase creation with the same client request ID and identical details to repair a failed approval handoff. Both first creation and identical retries return 201 because the current RPC does not report whether it inserted. Changed details with the same key return 409. Repeat the same decision to repair failed propagation to the purchase service; an opposing terminal decision returns 409. A 503/504 can occur after a write committed, so a failure response does not imply rollback. A subsequent purchase GET shows the propagated purchase status; the decision response describes the approval record.

## CORS and timeouts

`VUE_ORIGIN` defaults to exactly `http://localhost:5173`. Only an http(s) origin with a loopback hostname/IP is configurable; wildcards, remote hosts, paths, and credentials are rejected at startup. `http://127.0.0.1:5173` is a different origin and must be configured explicitly if used. Allowed preflights return 204, authorize only the route's GET/POST methods and `Content-Type`/`X-CSRF-Token`, and enable credentials only for the configured origin. Requests with another `Origin` return 403. Requests without an Origin, such as local curl commands, may authenticate normally with cookies and CSRF. CORS does not establish user identity or replace authentication.

Shell environment settings (no `.env` auto-loading):

- `API_GATEWAY_HTTP_ADDR=127.0.0.1:8080`.
- `PURCHASE_SERVICE_GRPC_TARGET=localhost:50051`, `APPROVAL_SERVICE_GRPC_TARGET=localhost:50052`.
- `VUE_ORIGIN=http://localhost:5173`.
- `GRPC_CALL_TIMEOUT=2s`, applied to each outgoing call and bounded by caller cancellation.
- `HTTP_MAX_BODY_BYTES=65536` (64 KiB; configurable from 1 byte to 1 MiB).
- `HTTP_READ_HEADER_TIMEOUT=5s`, `HTTP_READ_TIMEOUT=10s`.
- `HTTP_WRITE_TIMEOUT=15s`, `HTTP_IDLE_TIMEOUT=60s`.
- `SHUTDOWN_TIMEOUT=5s`. HTTP headers are limited to 16 KiB.

The header timeout must not exceed the read timeout, and the write timeout must exceed the RPC timeout. HTTP socket timeouts can close a slow or disconnected connection before an application JSON response can be delivered.

## Verification

```powershell
go test ./internal/gateway ./internal/config
go test ./...
go vet ./...
go build ./...
```

Handler tests use fake gRPC clients to verify request translation, validation, body limits, CORS/preflights, status mapping, redaction, cancellation, and deadlines. Server tests check timeout wiring and graceful request draining. The existing database tests still require `TEST_DATABASE_URL` and explicitly skip when it is unavailable.

The standard library provides the required routing and JSON controls: [net/http](https://pkg.go.dev/net/http#ServeMux), [encoding/json](https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields). Error mapping follows the meanings of [gRPC status codes](https://grpc.io/docs/guides/status-codes/).
