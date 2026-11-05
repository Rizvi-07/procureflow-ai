# ProcureFlow AI

ProcureFlow AI is an interview-ready portfolio project for an intelligent procurement management platform. It will let employees submit purchase requests, help reviewers manage approvals, and answer procurement policy questions using fictional documents.

The project is built in small, independently testable milestones, with maintainable code and explanations of important Go and gRPC concepts.

## Current status

**Phase 06: local Docker Compose deployment.** The Vue 3 + TypeScript dashboard, REST gateway, two Go gRPC services, and PostgreSQL run together with health checks and automatic migrations. Purchase/approval state lives in a named volume. Native development still supports PostgreSQL or in-memory repositories. **Demo identity only: `manager_id` is not authenticated, and manager/comment metadata is not persisted.** Authentication and AI remain future work.

See [the backend run guide](docs/backend.md) for two-terminal startup, configuration, a smoke client, tests, and Protocol Buffer generation.

Start the HTTP gateway with `go run ./cmd/api-gateway` once both gRPC services are running. See [the API guide](docs/api.md) for all five routes and curl/PowerShell examples.

## Technology stack

| Area | Planned technology |
| --- | --- |
| Backend services and API gateway | Go |
| Frontend | Vue.js 3, TypeScript, Vite |
| Internal service communication | gRPC and Protocol Buffers |
| Browser communication | REST/JSON through the Go API gateway |
| Database | PostgreSQL |
| Local infrastructure | Docker and Docker Compose |
| AI extension | Retrieval-augmented generation (RAG) with pgvector |
| Verification | Go unit/integration tests and frontend build checks |

## Functionality

- Implemented: create, get, and list purchases over gRPC, with validation and integer-cent calculations.
- Implemented: create, get, list, approve, or reject approvals; synchronize the purchase through gRPC.
- Implemented: retry-safe operations, dependency deadlines, structured logs, and health checks.
- Implemented: REST/JSON gateway for listing, reading, creating, and manually deciding purchases.
- Implemented: responsive Vue dashboard, purchasing forms, manual review, refresh, filtering, and clear loading/error states.
- Implemented: PostgreSQL persistence through pgxpool, isolated purchase/approval schemas, and versioned migrations.
- Implemented: multistage container images, private database/gRPC networking, migration-gated startup, and persistent local Docker Compose deployment.
- Planned: a policy assistant using fictional documents.

## Repository layout

```text
frontend/                    Vue 3 + TypeScript application and tests
  src/lib/                   Typed REST client and exact integer-cents helpers
  src/components/            Accessible purchase and manager forms
  src/composables/           Local reactive purchase state (no state library)
  tests/                     Opt-in live gateway acceptance checks
api-gateway/                 Reserved Phase 00 directory
cmd/
  api-gateway/               Go REST/JSON entry point
  purchase-service/          Purchase server entry point
  approval-service/          Approval server entry point
  smoke/                     Create -> approve -> read demonstration
  migrate/                   Apply PostgreSQL migrations
  healthcheck/               Deadline-bound container HTTP/gRPC health probe
internal/
  gateway/                   HTTP handlers, gRPC clients, and gateway tests
  purchase/                  Purchase rules and memory/PostgreSQL repositories
  approval/                  Approval rules and memory/PostgreSQL repositories
  postgres/                  Connection pool, error mapping, and database tests
  domain/                    Shared status values, validation, and errors
  rpc/                       gRPC handlers, client adapters, and integration tests
  config/                    Environment configuration
  server/                    Health checks, logging, and graceful shutdown
  app/                       Server dependency wiring and signal handling
proto/procureflow/v1/        Versioned Protocol Buffer contract
gen/procureflow/v1/          Generated Go messages and gRPC stubs
services/                    Reserved directories from Phase 00
migrations/                  Embedded, versioned SQL migrations (active)
database/migrations/         Unused Phase 00 placeholder
docs/
  architecture.md           Current system design and service boundaries
  backend.md                Runnable backend guide
  api.md                    REST API, DEMO-ONLY decisions, and HTTP examples
  docker.md                 Compose startup, logs, networking, and persistence demo
Dockerfile                   Go multistage build with service/gateway/migration targets
docker-compose.yml           Local deployment with a persistent PostgreSQL volume
```

Empty application directories contain `.gitkeep` placeholders so Git can track them. Remove a placeholder when its directory gains real files.

## Development preparation

The backend requires **Go 1.26 or newer** and PostgreSQL 14+ for persistent mode (verified with PostgreSQL 18.3). The in-memory demo does not require PostgreSQL. Generated Protocol Buffer code is included, so `protoc` is needed only when changing the contract. See [PostgreSQL setup and testing](docs/postgresql.md).

Use `.env.example` as the configuration reference. Native Go servers read environment variables from the shell; they do **not** automatically load `.env`. Docker Compose automatically reads root `.env` and uses the separate `COMPOSE_` settings. Never commit local `.env` files, real credentials, or API keys.

## Docker Compose quickstart

With Docker Desktop running Linux containers, from the repository root:

```powershell
docker compose config
docker compose up -d --build
docker compose ps -a
```

Open **http://localhost:5173/**; the API is **http://localhost:8080**. Only these loopback ports are published. The public **DEVELOPMENT-ONLY** database defaults require no setup or AI keys. Migrations finish before services accept traffic; `migrate` exiting with code 0 is expected. Stop with `docker compose down`, which preserves the named database volume, and resume with `docker compose up -d --wait`.

See [the Docker guide](docs/docker.md) for exact PowerShell commands, port overrides, logs, host/container addresses, and a create/approve/restart/read persistence check. This deployment uses its own volume and preserves any existing native database under `tmp/`.

## Frontend quickstart

Use Node.js 22.13+ on the Node 22 line, or Node 24+. The frontend uses Vue's Composition API, Vite, and TypeScript with no UI or state-management library.

1. Start PostgreSQL if using persistent mode, then run the purchase and approval services in separate terminals following [the backend guide](docs/backend.md).
2. From the repository root, start the HTTP gateway in another terminal:

   ```powershell
   go run ./cmd/api-gateway
   ```

3. In a new terminal, start the frontend:

   ```powershell
   cd frontend
   npm install
   if (-not (Test-Path .env.local)) { Copy-Item .env.example .env.local }
   npm run dev
   ```

   The command preserves an existing `.env.local`. Its only setting is public `VITE_API_BASE_URL=http://127.0.0.1:8080`; never put passwords or API keys in a `VITE_` variable. Vite loads frontend environment files; restart it after changing the URL.

Open **http://localhost:5173/**. Use `localhost`, which matches the gateway's default `VUE_ORIGIN`; `127.0.0.1:5173` is a different origin. Vite uses a strict port so it cannot silently switch to a disallowed origin. To use another local origin, set the gateway's `VUE_ORIGIN` and matching Vite host/port explicitly.

Enter an employee ID, item, quantity, and EUR unit price. The frontend converts decimal text to integer cents with BigInt, accepts a dot or comma, rejects more than two decimal places, and rejects totals outside JavaScript's exact integer range. It never calculates money with `Math.round(euros * 100)`. Select **Review** on a pending row to approve/reject with a demo manager ID. Final-status rows have no review action. **Refresh** reloads gateway data.

The table abbreviates IDs and exposes full IDs to assistive technology. The current API does not return submitted dates, so that column honestly shows **Not provided**. No client-created timestamp is presented as a server submission date. Manager IDs/comments remain demo-only and are not retained by the backend.

Uncertain create failures preserve the original payload and `client_request_id`; **Retry request** resends them identically. Editing as a new request is an explicit action, with a reminder that the previous request may already exist. Failed decisions remain visible with instructions to retry the same decision or refresh. No API data is seeded into the dashboard.

Frontend checks, from `frontend/`:

```powershell
npm test
npm run build
```

`npm test` runs unit and DOM-based Vue component tests without a backend. The build runs `vue-tsc` before producing `frontend/dist/`. For an opt-in test of the real create/list/approve/reject/refresh flow, start the gateway and services, then run:

```powershell
$env:RUN_LIVE_API_TESTS = '1'
npm run test:live
```

The live test mounts the actual Vue application in a test DOM, calls the real gateway with the allowed local origin, and creates **two real fictional requests**, one approved and one rejected. It retains those records and does not reset the database. Without the explicit opt-in variable, the live checks are reported as skipped. These checks do not replace a visual browser review. Stop the dev server before using `npm run preview` to serve a production build on the same localhost origin.

Read [docs/architecture.md](docs/architecture.md) for the design. Local AI assistant guidance belongs in the ignored `AGENTS.md` file; keep additional agent and AI assistance documentation in the ignored `docs/agents/` and `docs/ai/` directories.

## Development roadmap

| Phase | Milestone | Expected verification |
| --- | --- | --- |
| 00 | Repository foundation and architecture documentation | Check directories, documentation, ignore rules, and Git diff |
| 01 | Original persistence-first plan superseded by the requested milestones | Persistence delivered in Phase 03 |
| 02 | Completed: runnable purchase and approval gRPC services with in-memory storage | Go unit tests, loopback gRPC integration tests, and executable smoke client |
| 03 | Completed: PostgreSQL persistence and service-owned schemas | Database constraint, migration, concurrency, and restart checks |
| 04 | Completed: Go REST/JSON gateway backed by gRPC | HTTP handler tests, live route checks, and Go build |
| 05 | Completed: accessible Vue procurement dashboard | Frontend unit/component tests, live API flows, TypeScript check, and production build |
| 06 | Completed: local Docker Compose deployment | Health checks, automatic migrations, live UI/API flows, and persistence across container recreation |

The requested Phase 05 frontend and Phase 06 infrastructure scopes supersede the original roadmap. Policy RAG and interview preparation remain future work. Each milestone should remain small enough to review and verify independently.

## Data and Git workflow

Use fictional purchase records, suppliers, users, and policies. Work only on the existing `main` branch. Do not initialize Git again or automatically create branches, pull requests, commits, pushes, resets, or merges. Propose Conventional Commit messages for the user to apply manually.
