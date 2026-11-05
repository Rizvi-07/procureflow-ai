# PostgreSQL persistence (Phase 03)

## Local setup

Use PostgreSQL 14 or newer; this implementation was verified with PostgreSQL 18.3. Create a dedicated local demo database with your PostgreSQL administration tools. The examples below contain placeholders, not credentials. Supply your own local URL in the shell; do not commit it. `.env` is not loaded automatically.

From the repository root, set `DATABASE_URL` and run the migration command:

```powershell
$env:DATABASE_URL = 'postgres://USER:PASSWORD@localhost:5432/procureflow_dev?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/migrate
```

Both runs should succeed. `sslmode=disable` is only for a local database. Use your server's TLS configuration for remote connections. The migration role needs permission to create schemas/tables and the migration ledger in `public`; the runtime services do not run migrations or create databases.

Set the same database URL in each server terminal, then start:

```powershell
# Terminal 1, with DATABASE_URL exported
go run ./cmd/purchase-service
```

```powershell
# Terminal 2, with DATABASE_URL exported
go run ./cmd/approval-service
```

Startup logs show `storage=postgresql`. Leaving `DATABASE_URL` empty selects memory. A configured but unreachable database fails startup rather than silently falling back to memory. `DATABASE_TIMEOUT` defaults to `5s` and bounds pool startup checks, queries, and transactions. Earlier request deadlines still win. Pools close after server shutdown drains active requests.

## Restart demonstration

Run the existing smoke client while both servers are available:

```powershell
go run ./cmd/smoke -request-id persistent-demo-1
```

Record the purchase ID. Stop both services, start them again with the same `DATABASE_URL`, and run the same command. It must return the original ID, total of 5000 cents, and APPROVED status. The smoke client itself does not connect to PostgreSQL. A PostgreSQL restart also preserves this data as long as its data directory is retained.

## Retained local demo database

The database created during Phase 03 verification is also used by the current local demo. Keep these ignored files while using that demo:

- `tmp/phase03-pg-<id>/` contains PostgreSQL's data directory, including saved purchases and approvals.
- `tmp/phase03-postgres.json` records that directory's path and port. It is local machine metadata, not an application configuration file.

Although the directory is named `tmp`, this retained cluster is active local database storage. Git ignores it, and a fresh clone does not include it. Do not include it in automatic temporary-file cleanup. The integration tests' separate `procureflow_test_<uuid>` databases are dropped by the tests; this retained demo cluster has a different lifecycle. Remove it only when intentionally discarding the local demo data, after stopping both services and PostgreSQL. Phase 06 now provides a separate named Docker volume through [Compose](docker.md); it neither imports nor removes this existing native storage.

From the repository root, inspect the saved cluster:

```powershell
$db = Get-Content -Raw tmp/phase03-postgres.json | ConvertFrom-Json
& 'C:\Program Files\PostgreSQL\18\bin\pg_ctl.exe' -D $db.cluster status
```

If it reports that no server is running, start it with the recorded port:

```powershell
& 'C:\Program Files\PostgreSQL\18\bin\pg_ctl.exe' -D $db.cluster -l "$($db.cluster)\server.log" -o "-h 127.0.0.1 -p $($db.port)" -w start
```

The retained demo database is `procureflow_demo`. Its local runtime roles are `pf_purchase` and `pf_approval`, each restricted to its own schema. In the purchase service terminal, load the metadata, set the URL, and start the service:

```powershell
$db = Get-Content -Raw tmp/phase03-postgres.json | ConvertFrom-Json
$env:DATABASE_URL = "postgres://pf_purchase@127.0.0.1:$($db.port)/procureflow_demo?sslmode=disable"
go run ./cmd/purchase-service
```

In the approval service terminal:

```powershell
$db = Get-Content -Raw tmp/phase03-postgres.json | ConvertFrom-Json
$env:DATABASE_URL = "postgres://pf_approval@127.0.0.1:$($db.port)/procureflow_demo?sslmode=disable"
go run ./cmd/approval-service
```

These URLs apply only to this loopback-only demo cluster, which was initialized with local trust authentication. Use the regular credential-based setup above for a separate development database. Existing migrations and records are retained; no initialization or reset is needed to restart this cluster.

## Schema and migrations

Active migrations live in top-level `migrations/`, as requested for Phase 03. The old `database/migrations/` directory is an unused Phase 00 placeholder.

- `001_purchase.sql` creates `purchase.requests`: UUID primary key, unique `client_request_id`, validated employee/item fields, BIGINT quantity and cents, status, and creation/update timestamps.
- `002_approval.sql` creates `approval.decisions`: UUID primary key, unique UUID `purchase_id`, status, and creation/update timestamps.
- All timestamps are TIMESTAMPTZ instants, with UTC configured on application connections. Initial timestamps come from PostgreSQL; a real decision change updates `updated_at`. Identical retries leave it unchanged.
- CHECK constraints require positive quantity, nonnegative prices/totals, an exact total, and PENDING/APPROVED/REJECTED statuses. PostgreSQL NUMERIC is used only to evaluate the exact multiplication constraint without overflow; no floating-point money is used.

The runner embeds sorted SQL files, obtains a transaction-scoped advisory lock, and applies schema plus ledger changes in one transaction. `public.procureflow_migrations` records file names and SHA-256 checksums. Already-applied files are skipped; modified applied files are rejected. Line endings are normalized for checksums across Windows/Linux. Add a new numbered migration for changes; do not edit an applied migration. There is no automatic down/reset command.

Both services may share a database but access only their own schema. The approval purchase ID has no cross-schema foreign key; gRPC remains the service boundary. A local demo can use one database role. For deployment, use separate runtime roles with USAGE and SELECT/INSERT/UPDATE only on the owning schema's tables, and a separate migration role.

## Transactions, idempotency, and retries

Repository interfaces accept `context.Context` and return errors, letting the same business services use either memory or PostgreSQL. PostgreSQL operations use parameterized SQL and short contexts.

Purchase creation uses a unique client request key. Concurrent inserts elect one stored purchase; identical retries return it, while different details return `AlreadyExists`. Approval creation similarly elects one record per purchase UUID. Generated purchase IDs are now UUIDv4 strings; the `.proto` files and generated stubs are unchanged. The approval's own database UUID and audit timestamps are internal storage details and are not added to the RPC messages.

Approval decisions and purchase status updates lock their own row inside a transaction. Only a PENDING record can move to a terminal status, an identical terminal status succeeds, and an opposing terminal status fails. Each transaction commits before any outbound gRPC call.

Two service updates are still not atomic. Purchase creation may commit before approval creation fails; retry `CreatePurchase` with the original key and details. An approval decision may commit while the purchase remains PENDING; retry `DecideApproval` with the same purchase ID and decision. Those repairs work after service restarts because the local write and idempotency key are durable. Timeouts and dropped connections can also hide a successful commit, so always retry identically. There is no background reconciliation/outbox in this phase. Permanent data inconsistencies require investigation; retries cannot reverse a conflicting terminal status.

## Database integration tests

`TEST_DATABASE_URL` must be a PostgreSQL URL pointing to a test server. Its role needs CREATEDB permission. Each test creates a uniquely named `procureflow_test_<uuid>` database, applies migrations, and drops only that generated database during cleanup. Existing application databases are never truncated or reset. Tests never fall back to `DATABASE_URL`.

```powershell
$env:TEST_DATABASE_URL = 'postgres://TEST_USER:TEST_PASSWORD@localhost:5432/postgres?sslmode=disable'
go test -v -count=1 ./internal/postgres
go test -count=1 ./...
go vet ./...
go build ./...
```

Four database tests verify persistence/idempotency after reconnect, migration reruns and SQL constraints, concurrent creates/decisions plus lock deadlines, and repairing a saved decision after a restart. Tests SKIP with an explicit `PostgreSQL integration NOT RUN` reason if the URL is unset, the server cannot be reached/authenticated, or the role lacks CREATEDB. Invalid URL syntax, migration failures, and failed assertions are failures, not skips. Use verbose output to distinguish them; a package-level PASS can include skipped database tests.

The existing unit and loopback gRPC tests continue to use memory and run without a database.

Reference: [pgxpool documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [PostgreSQL constraints](https://www.postgresql.org/docs/current/ddl-constraints.html).
