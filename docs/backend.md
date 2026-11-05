# Running the backend

## Prerequisites

Use Go 1.26 or newer. Run all commands from the repository root. Dependencies are pinned in `go.mod` and `go.sum`. Generated protobuf files are included. The default demo uses memory; for PostgreSQL persistence, follow [postgresql.md](postgresql.md) to set `DATABASE_URL` in both server terminals and run migrations before starting them.

```powershell
go mod download
```

## Start two servers

Terminal 1:

```powershell
go run ./cmd/purchase-service
```

Terminal 2:

```powershell
go run ./cmd/approval-service
```

New servers listen on `127.0.0.1:50051` and `127.0.0.1:50052` respectively. Either can start first; both must be available for the full workflow. Logs are JSON and include the service name, RPC method, status code, and duration. Request payloads are not logged. Press Ctrl+C in each terminal to shut down. In-flight requests drain for up to five seconds before forced cancellation.

In a third terminal, run:

```powershell
go run ./cmd/smoke -request-id interview-demo-1
go run ./cmd/smoke -request-id interview-demo-1
```

The client waits for both health endpoints, creates a fictional two-lamp purchase totaling 5000 cents, repeats creation, approves twice, and reads the purchase back. It exits nonzero on any failed check. Example first-run output (the ID varies):

```text
created purchase=<id> total_cents=5000 status=STATUS_PENDING
read purchase=<id> status=STATUS_APPROVED; create and decision retries succeeded
```

The second command returns the same purchase ID, already APPROVED. Omitting `-request-id` generates a new key. In memory mode, restarting clears data. With `DATABASE_URL` set, restarting either service preserves records and idempotency keys.

## Start the REST/JSON gateway

With both services running, open another terminal and run `go run ./cmd/api-gateway`. The gateway is the browser HTTP entry point at `http://127.0.0.1:8080`; it never connects to PostgreSQL. See [api.md](api.md) for all five routes, curl/PowerShell examples, CORS, timeouts, and the DEMO-ONLY decision endpoint.

## Configuration

Servers read shell environment variables; `.env` is a reference convention, not automatically loaded. Defaults:

- `PURCHASE_SERVICE_GRPC_ADDR=127.0.0.1:50051`: purchase listener.
- `APPROVAL_SERVICE_GRPC_ADDR=127.0.0.1:50052`: approval listener.
- `PURCHASE_SERVICE_GRPC_TARGET=localhost:50051`: destination for approval callbacks and the smoke client.
- `APPROVAL_SERVICE_GRPC_TARGET=localhost:50052`: destination for purchase handoffs and the smoke client.
- `GRPC_CALL_TIMEOUT=2s`: maximum duration of each service-to-service call.
- `SHUTDOWN_TIMEOUT=5s`: drain limit before forced shutdown.
- `DATABASE_URL` (empty): PostgreSQL URL; empty selects memory. Set it separately in each server terminal.
- `DATABASE_TIMEOUT=5s`: bounds database operations and startup connection checks.
- `TEST_DATABASE_URL` (empty): optional integration-test server URL with CREATEDB permission; never falls back to `DATABASE_URL`.

For different ports, set all four address/target variables consistently in each terminal before starting a server or the smoke client:

```powershell
$env:PURCHASE_SERVICE_GRPC_ADDR = '127.0.0.1:60051'
$env:APPROVAL_SERVICE_GRPC_ADDR = '127.0.0.1:60052'
$env:PURCHASE_SERVICE_GRPC_TARGET = 'localhost:60051'
$env:APPROVAL_SERVICE_GRPC_TARGET = 'localhost:60052'
$env:GRPC_CALL_TIMEOUT = '2s'
$env:SHUTDOWN_TIMEOUT = '5s'
```

Listen addresses may have an empty host; peer targets require a host. Ports must be 1-65535 and durations must be positive. Invalid configuration exits with a structured error. Plaintext gRPC and no authentication are intentional for this local exercise; bind to loopback when running on a shared network.

## Contract and validation

`PurchaseService` exposes `CreatePurchase`, `GetPurchase`, `ListPurchases`, and `SetApprovalStatus`. `ApprovalService` exposes `CreateApproval`, `GetApproval`, `ListApprovals`, and `DecideApproval`. Approval get/decision operations use `purchase_id` as the key.

IDs (`employee_id`, `client_request_id`, and lookup IDs) must contain 1-128 ASCII letters, digits, underscores, or hyphens, starting with a letter or digit. UUID strings satisfy this rule but are not required. Item names must be valid UTF-8, contain non-whitespace text, and have at most 200 characters. Quantity must be positive, unit price nonnegative, and their product must fit a signed 64-bit integer. Inputs are compared exactly for idempotency; whitespace is not silently normalized.

New purchase IDs are UUIDv4 strings; PostgreSQL stores them as UUID keys. In PostgreSQL mode, `CreateApproval` requires a purchase UUID. Get/decision operations using a syntactically valid non-UUID key return `NotFound`. Employee and client request IDs remain ordinary strings. RPC definitions and generated code are unchanged.

Error behavior:

- `InvalidArgument`: invalid input, unknown enum, PENDING decision, or calculation overflow.
- `NotFound`: missing purchase or approval on get/update/decision.
- `AlreadyExists`: create key reused with different valid purchase details.
- `FailedPrecondition`: attempt to change a terminal decision or purchase status.
- `Unavailable`: failed dependency call or temporary database failure. The local write may already exist; retry using the same key/input or decision.
- `DeadlineExceeded`: caller or dependency deadline expired. A local or remote write may have completed; retry identically.
- `Canceled`: caller canceled the operation; completion may be ambiguous after a write.
- `Internal`: unexpected local failure, with a generic client-facing message.

Dependency error details are not forwarded. An unsuccessful handoff returns instructions to repeat `CreatePurchase`; an unsuccessful decision propagation returns instructions to repeat `DecideApproval`. The client owns retries; this phase has no automatic background synchronization. Repeating creation does not itself re-propagate an existing approval decision; repeat the decision operation to repair that direction.

## Test and build

```powershell
go test ./...
go vet ./...
go build ./...
```

Tests cover input validation, overflow boundaries, concurrent idempotency, transition conflicts, short deadlines, shutdown, and real loopback gRPC exchanges between the two services. Failure tests simulate unavailable peers, lost responses, and deadline expiry, then assert successful synchronization on retry. They do not require fixed ports or an external server.

Database integration tests are separate and explicitly skip when `TEST_DATABASE_URL` is unset/unavailable or the test role cannot create isolated databases. Use `go test -v -count=1 ./internal/postgres` to see executed versus skipped checks. See [postgresql.md](postgresql.md) for setup; a successful Go test exit with SKIP lines is not proof that persistence passed.

Where CGO and a supported C compiler are installed:

```powershell
$env:CGO_ENABLED = '1'
go test -race ./...
```

The race detector needs that native toolchain on Windows; ordinary tests do not.

## Regenerate Protocol Buffer code

The checked-in code was generated with `protoc` 36.2, `protoc-gen-go` 1.36.12, and `protoc-gen-go-grpc` 1.6.2. Install matching plugins when editing contracts:

```powershell
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
$env:PATH = "$(go env GOPATH)\bin;$env:PATH"
protoc --go_out=. --go_opt=module=procureflow-ai --go-grpc_out=. --go-grpc_opt=module=procureflow-ai proto/procureflow/v1/procurement.proto
```

Protocol Buffers define the wire contract; these commands generate both message types and client/server interfaces. Implement server behavior in `internal/`, not in generated files. Keep field numbers stable and reserve removed numbers. See the [official Go gRPC quick start](https://grpc.io/docs/languages/go/quickstart/) and [Go gRPC API reference](https://pkg.go.dev/google.golang.org/grpc) for generator and client APIs.
