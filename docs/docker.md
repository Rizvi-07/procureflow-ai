# Local Docker Compose deployment (Phase 06)

Run these PowerShell commands from the repository root with Docker Desktop
running in **Linux containers** mode and Docker Compose v2 available. The stack
builds the existing MVP; it requires no host Go, Node, PostgreSQL, AI credentials,
or models. The first build downloads base images and dependencies.

## Build and start

```powershell
Set-Location D:\Agent_Project\procureflow-ai
docker version
docker compose version

# Optional: preserve any existing local configuration.
if (-not (Test-Path .env)) { Copy-Item .env.example .env }

docker compose config
docker compose up -d --build
docker compose ps
docker compose ps -a
```

Open **http://localhost:5173/**. The browser calls **http://localhost:8080**.
Use `localhost` for the frontend so its origin matches CORS. Five long-running
containers should become healthy; `migrate` should show **Exited (0)** in
`ps -a`. That exit is successful completion, not a failed server.

Compose works without `.env` using the public **DEVELOPMENT-ONLY** values in
`.env.example`: database/user `procureflow_dev`, password `procureflow_dev_only`.
These are disposable local demo credentials, not secrets suitable for another
environment. Compose reads root `.env` automatically; native Go commands still
require shell exports. Existing native `DATABASE_URL` and bind settings are not
used by Compose. Its overrides use the separate `COMPOSE_` variables.

This is a loopback-only demonstration with plaintext HTTP/gRPC and no real
authentication. **`manager_id` is DEMO-ONLY.** CORS is not authorization. The
shared local database role can access both schemas; service repository boundaries
still enforce ownership in code. Separate least-privilege runtime roles and a
migration role remain deployment hardening work.

## Addresses and ports

Only frontend `127.0.0.1:5173` and gateway `127.0.0.1:8080` are published.
PostgreSQL and both gRPC ports have no host bindings. The frontend joins the
`edge` network; PostgreSQL and services join the internal `backend` network.
Only the gateway joins both. The browser never connects to gRPC or PostgreSQL.

Inside Compose, services bind `0.0.0.0` and use Docker DNS names:

- Purchase gRPC: `purchase-service:50051`.
- Approval gRPC: `approval-service:50052`.
- Database: `postgres:5432`.

`localhost` inside a container means that same container, so it cannot identify
a peer. Conversely, Docker service names are not browser hostnames:
`VITE_API_BASE_URL` is built as `http://localhost:8080`, the gateway address
reachable from the **host browser**. Nginx serves the production Vite output on
container port 8080, mapped to host 5173. No Vite development server runs there.

To avoid occupied host ports, set `COMPOSE_FRONTEND_PORT` and
`COMPOSE_GATEWAY_PORT` in `.env`, or in the current PowerShell session:

```powershell
$env:COMPOSE_FRONTEND_PORT = '8081'
$env:COMPOSE_GATEWAY_PORT = '8082'
docker compose up -d --build
```

Then visit `http://localhost:8081`. Compose updates the gateway CORS origin and
frontend build argument together. Rebuild after changing the gateway port:
`VITE_` values are public build-time configuration, not runtime secrets. Native
Go defaults remain unchanged, and host gRPC servers can coexist because Compose
does not publish their ports. Stop a native gateway or Vite server if it already
occupies the chosen browser-facing ports.

## Startup, migrations, and images

Startup follows PostgreSQL TCP readiness, then the one-shot migration command,
then both gRPC health checks, then gateway HTTP health, then frontend HTTP health.
The migration's successful exit gates backend startup. The services have no
mutual startup dependency even though they call one another at runtime.

`migrations/` is embedded into the migration binary. The existing transactional
runner records checksums and skips applied migrations, so rerunning it does not
duplicate schema changes. Add new numbered migrations rather than editing
applied files. Use `docker compose up -d --build` after code/schema changes;
`restart` alone does not rebuild an image. To explicitly rerun the current
migration image while PostgreSQL is healthy:

```powershell
docker compose run --rm migrate
```

Do not send application traffic during schema maintenance. If migration fails,
inspect its logs and fix the cause; do not reset the database to bypass it.
The health checks prove process readiness, not cross-service synchronization or
ongoing database availability. Existing timeout errors and identical retries
still handle partial failures; see [architecture.md](architecture.md).

The root multistage Dockerfile shares one Go builder and produces four runtime
targets: purchase, approval, gateway, and migrations. A small Go `healthcheck`
command probes the existing standard gRPC health protocol or HTTP `/healthz`
with a deadline; it adds no business RPC. Runtime Go images are non-root with
read-only filesystems and dropped capabilities. The frontend uses a Node builder
and an unprivileged Nginx runtime with writable temporary memory only. Build
contexts exclude environment files, local database storage, agent guidance,
dependencies, and generated output through `.dockerignore`.

## Inspect, logs, and stop

```powershell
docker compose ps -a
docker compose logs --tail 100 postgres migrate
docker compose logs --tail 100 purchase-service approval-service api-gateway frontend
docker compose logs -f api-gateway purchase-service approval-service
# Ctrl+C exits log following; it does not stop the containers.

# Pause and resume existing containers:
docker compose stop
docker compose start

# Remove this project's containers/networks, retaining database storage:
docker compose down
docker compose up -d --wait
```

The named volume is **`procureflow-ai_postgres_data`** with the default project
name. PostgreSQL 18 mounts it at `/var/lib/postgresql`, with versioned PGDATA
beneath that path. Keep the project name and volume when restarting; a different
project name selects different storage. Never remove volumes as part of routine
shutdown. Changing the database user/password/name variables does not rewrite
an already-initialized database; keep them stable for this demo.

Compose storage is separate from any native Phase 03 cluster retained under
ignored `tmp/`. Existing native data is neither imported nor removed. Do not
delete that directory as build cleanup. See [postgresql.md](postgresql.md).

## Live flow and persistence check

With the default ports, run the following in **one PowerShell session**. It
creates a real fictional purchase, approves it, recreates the containers, then
checks durable state and idempotency. The shutdown briefly interrupts this
Compose project's UI/API and preserves its named volume.

```powershell
$ErrorActionPreference = 'Stop'
$base = 'http://localhost:8080'
$body = @{
    client_request_id = [guid]::NewGuid().ToString()
    employee_id = 'emp-compose-demo'
    item_name = 'Fictional demo laptop'
    quantity = 2
    unit_price_cents = 80000
} | ConvertTo-Json
$purchase = Invoke-RestMethod -Method Post -Uri "$base/api/purchases" -ContentType 'application/json' -Body $body
$id = $purchase.id
if ($purchase.status -ne 'PENDING' -or $purchase.total_cents -ne 160000) { throw 'Invalid purchase result' }
$decision = @{ manager_id = 'manager-demo'; decision = 'APPROVED'; comment = 'Fictional local demo' } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "$base/api/purchases/$id/decision" -ContentType 'application/json' -Body $decision
Invoke-RestMethod -Uri "$base/api/purchases/$id"
Invoke-RestMethod -Uri "$base/api/purchases"

docker compose down
if ($LASTEXITCODE -ne 0) { throw 'Compose shutdown failed' }
docker compose up -d --wait
if ($LASTEXITCODE -ne 0) { throw 'Compose startup failed' }

$saved = Invoke-RestMethod -Uri "$base/api/purchases/$id"
if ($saved.id -ne $id -or $saved.status -ne 'APPROVED' -or $saved.total_cents -ne 160000) { throw 'Persistence check failed' }
$replayed = Invoke-RestMethod -Method Post -Uri "$base/api/purchases" -ContentType 'application/json' -Body $body
if ($replayed.id -ne $id) { throw 'Idempotency check failed' }
# Also retries propagation of the persisted approval decision:
Invoke-RestMethod -Method Post -Uri "$base/api/purchases/$id/decision" -ContentType 'application/json' -Body $decision
"Persistence and idempotency verified for $id"
```

The existing opt-in frontend live tests can also target this running stack:

```powershell
Push-Location frontend
npm ci
$env:RUN_LIVE_API_TESTS = '1'
$env:VITE_API_BASE_URL = 'http://localhost:8080'
npm run test:live
Remove-Item Env:RUN_LIVE_API_TESTS
Remove-Item Env:VITE_API_BASE_URL
Pop-Location
```

These tests require host Node and create two additional fictional records using
the actual Vue forms and API (approve/reject/refresh). They are opt-in, not part
of container startup, and do not reset data. See [the frontend guide](../README.md#frontend-quickstart).

References: [Compose startup conditions](https://docs.docker.com/compose/how-tos/startup-order/),
[Compose networking](https://docs.docker.com/compose/how-tos/networking/),
[PostgreSQL image storage layout](https://hub.docker.com/_/postgres), and
[unprivileged Nginx image](https://hub.docker.com/r/nginx/nginx-unprivileged).
