# REST/JSON API gateway (Phase 04)

**DEMO-ONLY: `manager_id` is caller-supplied text, not an authenticated identity. Anyone able to reach this local gateway can submit a manual decision. Manager IDs and comments are validated but are not stored or forwarded by the existing approval RPC.** The response explicitly includes `demo_only: true` and `metadata_persisted: false`. This phase adds no chatbot approval integration or AI tools.

## Start the gateway

Run commands from the repository root with Go 1.26 or newer. Start the purchase and approval services as described in [backend.md](backend.md); persistent mode is covered in [postgresql.md](postgresql.md). Then open another terminal:

```powershell
go run ./cmd/api-gateway
```

The gateway listens on `http://127.0.0.1:8080`. It uses reusable gRPC connections to `localhost:50051` and `localhost:50052`; it does not load `DATABASE_URL` or access tables. New service processes bind to loopback by default. The browser's only REST/JSON endpoint is the gateway. There is no gRPC-Web endpoint or service-side CORS support.

The gateway can start before its peers. `/healthz` reports gateway process liveness only; API calls return controlled errors while a peer is unavailable. Ctrl+C drains HTTP requests for up to `SHUTDOWN_TIMEOUT` before forcing connections closed.

## Endpoints and JSON

- `GET /healthz` -> 200 with `{"status":"ok","service":"api-gateway"}`.
- `GET /api/purchases` -> 200 with `{"purchases":[...]}`. An empty list is `[]`, never `null`. No pagination is implemented.
- `GET /api/purchases/{id}` -> 200 with a purchase, or 404 if missing.
- `POST /api/purchases` -> 201 with a purchase and its relative `Location` header.
- `POST /api/purchases/{id}/decision` -> 200 with the saved approval status and the demo-only indicators.

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
{"purchase_id":"<purchase UUID>","status":"APPROVED","demo_only":true,"metadata_persisted":false}
```

The existing `DecideApproval` contract carries only purchase ID and status. Manager ID/comment are neither audit evidence nor persisted metadata; they are not logged. The gateway does not change protobuf contracts or add approval rules.

## PowerShell examples

This sequence exercises all five routes with fictional data:

```powershell
$base = 'http://127.0.0.1:8080'
Invoke-RestMethod "$base/healthz"
Invoke-RestMethod "$base/api/purchases"

$body = @{
  client_request_id = [guid]::NewGuid().ToString()
  employee_id = 'emp-101'
  item_name = 'Laptop'
  quantity = 2
  unit_price_cents = 80000
} | ConvertTo-Json
$purchase = Invoke-RestMethod -Uri "$base/api/purchases" -Method Post -ContentType 'application/json' -Body $body
Invoke-RestMethod "$base/api/purchases/$($purchase.id)"

$decision = @{ manager_id = 'manager-demo'; decision = 'APPROVED'; comment = 'Within budget' } | ConvertTo-Json
Invoke-RestMethod -Uri "$base/api/purchases/$($purchase.id)/decision" -Method Post -ContentType 'application/json' -Body $decision
Invoke-RestMethod "$base/api/purchases/$($purchase.id)"
```

## curl examples

These examples use POSIX shell quoting. On Windows, use the PowerShell sequence above; `curl.exe` is the native curl command, while Windows PowerShell may alias `curl` to `Invoke-WebRequest`.

```sh
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/api/purchases
curl -i http://127.0.0.1:8080/api/purchases \
  -H 'Content-Type: application/json' \
  -d '{"client_request_id":"7e81c280-2204-47bb-89c8-8d777a25a730","employee_id":"emp-101","item_name":"Laptop","quantity":2,"unit_price_cents":80000}'

# Copy the id from the create response:
PURCHASE_ID='<purchase UUID>'
curl -i "http://127.0.0.1:8080/api/purchases/$PURCHASE_ID"
curl -i "http://127.0.0.1:8080/api/purchases/$PURCHASE_ID/decision" \
  -H 'Content-Type: application/json' \
  -d '{"manager_id":"manager-demo","decision":"APPROVED","comment":"Within budget"}'
curl -i "http://127.0.0.1:8080/api/purchases/$PURCHASE_ID"
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
- `Unauthenticated` -> 401; `PermissionDenied` -> 403 (reserved for future authenticated backends).
- `ResourceExhausted` -> 429; `Unavailable` -> 503; `DeadlineExceeded` -> 504.
- `Canceled` -> 408 if the client is still connected.
- Internal, unknown, unimplemented, data-loss, or malformed peer responses -> controlled 502; an unexpected gateway panic -> 500.

Peer error messages are not exposed to clients or request logs. Local validation errors describe the invalid field. Retryable errors include `Retry-After: 1`; the gateway never retries mutations automatically.

Repeat purchase creation with the same client request ID and identical details to repair a failed approval handoff. Both first creation and identical retries return 201 because the current RPC does not report whether it inserted. Changed details with the same key return 409. Repeat the same decision to repair failed propagation to the purchase service; an opposing terminal decision returns 409. A 503/504 can occur after a write committed, so a failure response does not imply rollback. A subsequent purchase GET shows the propagated purchase status; the decision response describes the approval record.

## CORS and timeouts

`VUE_ORIGIN` defaults to exactly `http://localhost:5173`. Only an http(s) origin with a loopback hostname/IP is configurable; wildcards, remote hosts, paths, and credentials are rejected at startup. `http://127.0.0.1:5173` is a different origin and must be configured explicitly if used. Allowed preflights return 204, authorize only the route's GET/POST methods and `Content-Type`, and never enable credentials. Requests with another `Origin` return 403. Requests without an Origin, such as local curl commands, are accepted. CORS does not establish user identity or replace authentication.

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
