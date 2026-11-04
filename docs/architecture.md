# ProcureFlow AI architecture

## Current implementation: Phase 03

The runnable backend has two Go processes communicating over gRPC. Each service depends on a context-aware repository interface. Without `DATABASE_URL`, it uses memory; with that variable set, it uses pgxpool and its own PostgreSQL schema. Both executables belong to one root Go module. Domain logic lives in `internal/purchase` and `internal/approval`; `cmd/` contains small entry points. The Protocol Buffer contracts are unchanged.

```mermaid
flowchart LR
    Client[Go smoke client] -->|Create / Get / List| Purchase[Purchase gRPC service :50051]
    Client -->|Decide / Get / List| Approval[Approval gRPC service :50052]
    Purchase -->|CreateApproval with deadline| Approval
    Approval -->|SetApprovalStatus with deadline| Purchase
    Purchase -->|owned schema only| PurchaseStore[(purchase.requests)]
    Approval -->|owned schema only| ApprovalStore[(approval.decisions)]
```

The two schemas may share one PostgreSQL instance for the local demo. There are no cross-schema queries or foreign keys between service data. Docker, the REST/JSON gateway, frontend, authentication, and policy RAG remain future work. The original `services/` directories remain reserved; current backend code is under `cmd/` and `internal/`.

## Boundaries and Go packages

- `internal/purchase` owns purchases, price calculations, create keys, and purchase status transitions.
- `internal/approval` owns approval records and decisions, keyed by purchase ID.
- `internal/domain` defines shared status values, validation helpers, and sentinel errors. It has no gRPC dependency.
- `internal/rpc` translates generated messages into domain inputs and maps domain errors into gRPC status codes. Client adapters implement the small interfaces consumed by the services.
- `internal/config` reads environment variables and rejects invalid addresses or durations.
- `internal/postgres` manages connection pools and sanitizes database errors; it does not access service tables.
- `migrations/` contains embedded SQL and the migration runner; only `cmd/migrate` performs schema changes.
- `internal/server` supplies structured request logs, health registration, and bounded graceful shutdown.
- `internal/app` wires repositories, services, clients, and handlers together and handles process signals.
- `proto/procureflow/v1/procurement.proto` is the versioned contract; `gen/procureflow/v1` contains generated Go code, never hand-edited.

A Go interface describes the behavior a caller needs. For example, purchase logic needs only `CreateApproval(ctx, purchaseID)`. Unit tests can supply a small fake; the running application supplies a gRPC client adapter. Business logic does not need generated protobuf types or a network connection to be tested.

## Purchase creation and partial failure

1. Validate IDs, item name, positive quantity, and nonnegative unit price.
2. Check `unit_price_cents <= MaxInt64 / quantity` before multiplying. Prices use integer cents, so no floating-point rounding is involved.
3. Atomically insert a PENDING purchase or return the existing record for the same `client_request_id` and identical input. Changed input with the same key returns `AlreadyExists`.
4. Release the repository lock and call `CreateApproval` with a short child-context deadline.
5. Return the current purchase after the call succeeds. A failure leaves the purchase saved and returns a retryable transport error with retry instructions.

An identical create retry uses the same purchase ID and calls the idempotent approval operation again. `CreateApproval` atomically creates at most one record per purchase ID and never resets an existing decision. This repairs a failed handoff or lost response without creating a duplicate. Returned purchase status may already be terminal if a reviewer decided it between calls.

## Approval decisions and synchronization

Only PENDING -> APPROVED or PENDING -> REJECTED is allowed. Repeating the same terminal decision is safe; switching to the other terminal decision returns `FailedPrecondition`.

The approval service records the decision locally before calling `SetApprovalStatus` on the purchase service. A failed call does not roll back that decision. Repeating `DecideApproval` with the same ID and status always retries propagation. The purchase service also accepts an identical status update and rejects a conflicting one.

There is no cross-service transaction. During a failure, reads can show a decided approval and a still-PENDING purchase. Recovery depends on the caller retrying the same operation; there is no background queue or reconciliation worker. A timeout is ambiguous: the peer may have completed the write before its response was lost.

## Concurrency and contexts

Memory repositories use `sync.RWMutex` and return value snapshots. PostgreSQL repositories use unique constraints and `INSERT ... ON CONFLICT DO NOTHING` for creates. A separate query reads the committed winner after a duplicate insert; changed input still returns `AlreadyExists`.

Decision updates begin a transaction, read the owned record with `SELECT ... FOR UPDATE`, check its state, update if needed, and commit. The row lock serializes concurrent decisions across processes. Identical decisions do not rewrite timestamps. Transactions and mutexes are released before gRPC calls; a database transaction cannot make two independent service commits atomic.

The database stores UUID primary keys, integer BIGINT cents, and TIMESTAMPTZ audit timestamps. Connections set their time zone to UTC. CHECK constraints reject invalid quantities, amounts, totals, and statuses. `quantity::numeric * unit_price_cents::numeric` validates the total exactly without overflowing during the check; stored monetary values remain integer cents.

`context.Context` carries cancellation and deadlines. Each database operation and service-to-service call derives a bounded child context; an earlier caller deadline still wins. Failed transactions roll back with a short cleanup context even if the request was canceled. The application closes its pool after draining requests. Generated gRPC clients handle Protocol Buffer serialization and transport, while services supply validation and domain decisions.

Both servers expose the standard gRPC health service. Health indicates that the process serves RPCs; it does not assert that the peer is reachable or that all records are synchronized. Shutdown drains requests and falls back to forced cancellation after the configured timeout.

## Intentional limitations

PostgreSQL mode preserves data and idempotency keys across restarts. It still requires the caller to retry failed cross-service synchronization: a durable APPROVED decision may coexist with a PENDING purchase until the identical decision is retried. There is no outbox or background repair worker. Memory mode remains ephemeral. Lists return all records with no pagination. `CreateApproval` validates ID syntax (a UUID in PostgreSQL mode) but does not verify purchase existence; its intended caller is the purchase service. There is no authorization around internal status operations.

Transport is plaintext for local development. The default bind addresses (`:50051`, `:50052`) listen on all interfaces; use `127.0.0.1:50051` and `127.0.0.1:50052` to restrict listeners locally. Schema ownership is enforced by repository query boundaries; production deployment should also grant separate runtime database roles access only to their own schemas.

See [backend.md](backend.md) and [postgresql.md](postgresql.md) for commands, migration behavior, and verification.
