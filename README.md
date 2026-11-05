# ProcureFlow AI

ProcureFlow AI is an interview-ready portfolio project for an intelligent procurement management platform. It will let employees submit purchase requests, help reviewers manage approvals, and answer procurement policy questions using fictional documents.

The project is built in small, independently testable milestones, with maintainable code and explanations of important Go and gRPC concepts.

## Current status

**Phase 08: optional read-only policy RAG.** The procurement MVP remains independent of AI credentials and models. An opt-in gRPC assistant, gateway endpoint, and collapsed Vue panel answer fictional policy questions through a separate pgvector index and configured OpenAI provider. **Live provider integration is unverified:** tests use fake providers with real pgvector, not paid calls. **Local authentication is implemented:** employees see their own requests; managers review other people's requests. Manager/comment audit metadata is not persisted. Hosted CI status requires an actual GitHub run. See [account setup](docs/authentication.md).

See [the backend run guide](docs/backend.md) for two-terminal startup, configuration, a smoke client, tests, and Protocol Buffer generation.

Start the HTTP gateway with `go run ./cmd/api-gateway` once both gRPC services are running. See [the API guide](docs/api.md) for procurement, signup/login, admin routes and curl/PowerShell examples.

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
- Implemented: PostgreSQL users, employee signup, admin role assignment, server-side sessions, CSRF and service-enforced permissions.
- Implemented: responsive Vue dashboard, purchasing forms, manual review, refresh, filtering, and clear loading/error states.
- Implemented: PostgreSQL persistence through pgxpool, isolated purchase/approval schemas, and versioned migrations.
- Implemented: multistage container images, private database/gRPC networking, migration-gated startup, and persistent local Docker Compose deployment.
- Implemented: optional policy-only RAG with bounded retrieval, exact source/quote checks, repeatable imports, and explicit Not configured behavior. Live OpenAI integration remains unverified.

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
  auth-setup/                One-time ignored random local credentials
  auth-seed/                 Explicit insert-only SQL account bootstrap
  purchase-service/          Purchase server entry point
  approval-service/          Approval server entry point
  smoke/                     Create -> approve -> read demonstration
  migrate/                   Apply PostgreSQL migrations
  healthcheck/               Deadline-bound container HTTP/gRPC health probe
  assistant-service/         Optional policy-only gRPC service; no purchase actions
  policy-import/             Explicit optional schema/import command; can incur API cost
internal/
  gateway/                   HTTP handlers, sessions, gRPC clients, and gateway tests
  auth/                      SQL users, role rules/audit, sessions and signed identities
  purchase/                  Purchase rules and memory/PostgreSQL repositories
  approval/                  Approval rules and memory/PostgreSQL repositories
  postgres/                  Connection pool, error mapping, and database tests
  domain/                    Shared status values, validation, and errors
  rpc/                       gRPC handlers, client adapters, and integration tests
  config/                    Environment configuration
  server/                    Health checks, logging, and graceful shutdown
  app/                       Server dependency wiring and signal handling
  assistant/                 Chunking, retrieval, provider adapter, and tests
proto/procureflow/v1/        Versioned Protocol Buffer contract
gen/procureflow/v1/          Generated Go messages and gRPC stubs
services/                    Reserved directories from Phase 00
migrations/                  Embedded, versioned SQL migrations (active)
  assistant/                 Separate optional AI migration, not run by the MVP
policies/                    Three synthetic FICTIONAL Markdown policy documents
proto/assistant/v1/          Separate policy-only RPC contract
gen/assistant/v1/            Generated policy messages and gRPC stubs
database/migrations/         Unused Phase 00 placeholder
docs/
  architecture.md           Current system design and service boundaries
  backend.md                Runnable backend guide
  api.md                    REST API, authenticated decisions, and HTTP examples
  docker.md                 Compose startup, logs, networking, and persistence demo
  policy-assistant.md       Optional RAG setup, privacy, cost, and verification
Dockerfile                   Go multistage build with service/gateway/migration targets
docker-compose.yml           Local deployment with a persistent PostgreSQL volume
scripts/smoke.ps1             Repeatable real HTTP create/read/list/approve/retry check
.github/workflows/ci.yml      Main push/manual Go, PostgreSQL/pgvector, and frontend checks
```

Empty application directories contain `.gitkeep` placeholders so Git can track them. Remove a placeholder when its directory gains real files.

## Development preparation

The backend requires **Go 1.26 or newer** and PostgreSQL 14+ for persistent mode (verified with PostgreSQL 18.3). The direct gRPC memory demo does not require PostgreSQL; the browser app now requires PostgreSQL for users, signup and roles. Generated Protocol Buffer code is included, so `protoc` is needed only when changing the contract. See [PostgreSQL setup and testing](docs/postgresql.md).

Use `.env.example` as the configuration reference. Native Go servers read environment variables from the shell; they do **not** automatically load `.env`. Docker Compose automatically reads root `.env` and uses the separate `COMPOSE_` settings. Never commit local `.env` files, real credentials, or API keys.

## Accounts

Users now live in PostgreSQL **auth.users**. Choose **Create an account** on the login screen to sign up as an employee. Sign in as **admin.demo** using the password in ignored **secrets/admin-credentials.json**, then open **Manage users** to assign employee, manager or admin. The last admin cannot be demoted. Changing a role invalidates that user's old sessions.

Existing employee.demo, employee.two and manager.demo credentials still work. For an existing installation, generate only the new admin files with `go run ./cmd/auth-setup -admin-only`, then rebuild/start Compose. Fresh installations use `go run ./cmd/auth-setup`. See [the authentication guide](docs/authentication.md) for exact setup and limitations.

## Docker Compose quickstart

With Docker Desktop running Linux containers, from the repository root:

```powershell
# Fresh install only. Existing demo: use -admin-only to add missing admin files.
# Skip setup entirely if the required files already exist.
go run ./cmd/auth-setup
docker compose config
docker compose up -d --build
docker compose ps -a
```

Open **http://localhost:5173/**; the API is **http://localhost:8080**. Only these loopback ports are published. The public **DEVELOPMENT-ONLY** database defaults require no database setup or AI keys. Generate the ignored account files once before starting; skip setup on later runs. Migrations finish before services accept traffic; `migrate` and `auth-seed` exiting with code 0 is expected. SQL account passwords and admin-assigned roles are preserved on subsequent starts. Stop with `docker compose down`, which preserves the named database volume, and resume with `docker compose up -d --wait`.

See [the Docker guide](docs/docker.md) for exact PowerShell commands, port overrides, logs, host/container addresses, and a create/approve/restart/read persistence check. This deployment uses its own volume and preserves any existing native database under `tmp/`.

## Optional policy assistant

Default Compose startup runs no AI services and requires no API key or models. The Vue panel is collapsed and makes no automatic requests. POST `/api/assistant/ask` reports **Not configured** while disabled. The assistant has no purchase/approval clients, tools, or live order lookup capability.

Read [the optional RAG guide](docs/policy-assistant.md) before enabling the `assistant` profile or running `policy-import`: real imports/questions incur provider cost and send synthetic text to OpenAI. It documents exact commands, model/dimension consistency, citation checks, separate storage, privacy, and limitations. No paid calls run in CI; live OpenAI RAG remains **unverified** until explicitly configured and exercised.

## Frontend quickstart

Use Node.js 22.13+ on the Node 22 line, or Node 24+. The frontend uses Vue's Composition API, Vite, and TypeScript with no UI or state-management library.

1. Prepare PostgreSQL identity storage and bootstrap accounts using [the authentication guide](docs/authentication.md#native-development), then run the purchase and approval services in separate terminals following [the backend guide](docs/backend.md).
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

   The command preserves an existing `.env.local`. Its only setting is public `VITE_API_BASE_URL=http://localhost:8080`; never put passwords or API keys in a `VITE_` variable. Vite loads frontend environment files; restart it after changing the URL.

Open **http://localhost:5173/**. Use `localhost`, which matches the gateway's default `VUE_ORIGIN`; `127.0.0.1:5173` is a different origin. Vite uses a strict port so it cannot silently switch to a disallowed origin. To use another local origin, set the gateway's `VUE_ORIGIN` and matching Vite host/port explicitly.

Sign in with a generated local account. Employee ID is bound to the signed-in account. Enter an item, quantity, and EUR unit price. The frontend converts decimal text to integer cents with BigInt, accepts a dot or comma, rejects more than two decimal places, and rejects totals outside JavaScript's exact integer range. It never calculates money with `Math.round(euros * 100)`. Sign in as a manager and select **Review** on another person's pending row to approve/reject. Employee accounts and a manager's own rows have no review action. Final-status rows have no review action. **Refresh** reloads gateway data.

The table abbreviates IDs and exposes full IDs to assistive technology. The current API does not return submitted dates, so that column honestly shows **Not provided**. No client-created timestamp is presented as a server submission date. Manager identity is verified; manager IDs/comments are not retained as an audit record by the backend.

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

The live tests mount the actual Vue application in a test DOM, call the real gateway with the allowed local origin, and create **two real fictional requests**, one approved and one rejected. A third test exercises signup and admin role assignment, leaving its synthetic user as an employee. It retains those records and does not reset the database. Without the explicit opt-in variable, the live checks are reported as skipped. These checks do not replace a visual browser review. Stop the dev server before using `npm run preview` to serve a production build on the same localhost origin.

Read [docs/architecture.md](docs/architecture.md) for the design. Local AI assistant guidance belongs in the ignored `AGENTS.md` file; keep additional agent and AI assistance documentation in the ignored `docs/agents/` and `docs/ai/` directories.

## Repeatable validation and CI

From the repository root in PowerShell, verify formatting without changing files,
then run Go checks. PowerShell does not automatically fail on a native command's
nonzero exit code, so check `$LASTEXITCODE` before continuing:

```powershell
$unformatted = @(gofmt -l cmd internal gen migrations)
if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
if ($unformatted.Count -gt 0) { throw "Run gofmt on: $($unformatted -join ', ')" }
go vet ./...
if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
go test -v -count=1 ./...
if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
go build ./...
if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }

Push-Location frontend
try {
    npm ci
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
    npm test
    if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed' }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend typecheck/build failed' }
} finally { Pop-Location }
```

Without `TEST_DATABASE_URL`, the six procurement/identity PostgreSQL integration tests explicitly
**SKIP**. The optional pgvector test also skips without `AI_TEST_DATABASE_URL`;
the unit and loopback gRPC tests still run. For full local database
coverage, set a URL for a separate test server whose role has CREATEDB before
running the Go checks. Each integration test creates, migrates, and drops only
its own generated database. See [the test database guide](docs/postgresql.md#database-integration-tests).
Never point test setup at a production server.

For the real HTTP flow, start the stack and run:

```powershell
docker compose config --quiet
if ($LASTEXITCODE -ne 0) { throw 'Invalid Compose configuration' }
docker compose up -d --build --wait
if ($LASTEXITCODE -ne 0) { throw 'Compose startup failed' }
powershell.exe -NoProfile -File ./scripts/smoke.ps1
if ($LASTEXITCODE -ne 0) { throw 'Procurement smoke check failed' }
powershell.exe -NoProfile -File ./scripts/auth-smoke.ps1
if ($LASTEXITCODE -ne 0) { throw 'Smoke check failed' }
# If the gateway uses a different host port:
# powershell.exe -NoProfile -File ./scripts/smoke.ps1 -BaseUrl http://localhost:8082 -TimeoutSeconds 10
```

On PowerShell 7 or another OS, use `pwsh -NoProfile -File ./scripts/smoke.ps1`
instead. The script uses `Invoke-RestMethod`, a fresh client UUID each run, and
bounded requests. It creates a real fictional 5000-cent purchase, checks GET
and list, approves it, reads APPROVED, repeats the identical decision, and reads
again. Any HTTP error or assertion failure exits nonzero. It leaves the record
for inspection and never resets data or removes volumes. It signs in with generated employee/manager accounts, also leaves a manager-owned PENDING security-check record, and verifies isolation, CSRF and self-review restrictions. The additional auth-smoke.ps1 checks public signup, admin role grants, session revocation and delegated approval, leaving its synthetic account as an employee. Add -RestartGateway to that script to check persistence across a gateway restart. The scripts target either Compose or already-running
native services through their gateway; it does not manage their lifecycle.

`.github/workflows/ci.yml` runs on **pushes to `main` and manual dispatch** only;
both jobs also require the `main` ref. It has read-only repository permissions
and does not publish, deploy, commit, or push. The backend job uses the Go
version in `go.mod`, verifies `gofmt`, runs vet/tests/build, and provisions an
PostgreSQL 18 service plus a separate pgvector service for the assistant, with
explicit test URLs. Their public CI-only password is a fixture, not a
repository secret or development database credential. The role can create the
isolated test databases. Verbose test output is checked for unexpected database
skips, which fail the job. The frontend job uses Node 24, installs from the lock
file with `npm ci`, runs the frontend unit/component tests, and runs the TypeScript
check plus Vite build. Actions configuration follows the official
[Go setup](https://github.com/actions/setup-go/tree/v6),
[Node setup](https://github.com/actions/setup-node/tree/v6), and
[PostgreSQL service](https://docs.github.com/en/actions/tutorials/use-containerized-services/create-postgresql-service-containers)
guides.

After manually committing and pushing, inspect the **CI** workflow in GitHub's
Actions tab, including both jobs and database test output. Manual **Run
workflow** requires this workflow to exist on the default branch. Until a real
run finishes successfully, GitHub CI status is **unverified**.

Known coverage gaps: CI does not build Compose images, run the HTTP smoke script
against a deployed stack, check persistence across container recreation, or run
the opt-in live Vue tests. Run those locally using this section and
[the Docker persistence walkthrough](docs/docker.md#live-flow-and-persistence-check).
Unit/component tests use a DOM simulator rather than a full browser; visual
accessibility review, browser automation, load testing, and race-detector checks
are not configured in CI. Fake-provider tests include real pgvector retrieval;
paid OpenAI calls and live model-quality evaluation are excluded. Tests cover deterministic deadlines and partial-failure
retries, but there is still no background repair worker. Signup, admin role grants, role-change session invalidation and concurrent last-admin protection are covered; production identity federation, MFA, password reset and durable shared sessions are not implemented.

PostgreSQL accounts, employee signup and admin role assignment were added after Phase 08; setup and limitations are in [authentication.md](docs/authentication.md).

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
| 07 | Repeatable validation and main-only GitHub Actions configuration | Real HTTP smoke script, focused failure tests, Go/PostgreSQL checks, and frontend checks; hosted CI result awaits a run |
| 08 | Optional read-only policy RAG | MVP baseline checks, real pgvector/source matching with fake providers, and disabled/unavailable assistant checks; live integration unverified |

The requested milestones supersede the original roadmap. Live provider evaluation, production hardening, and interview preparation remain future work. Each milestone should remain small enough to review and verify independently.

## Data and Git workflow

Use fictional purchase records, suppliers, users, and policies. Work only on the existing `main` branch. Do not initialize Git again or automatically create branches, pull requests, commits, pushes, resets, or merges. Propose Conventional Commit messages for the user to apply manually.
