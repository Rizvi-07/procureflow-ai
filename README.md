# ProcureFlow AI

ProcureFlow AI is an interview-ready portfolio project for an intelligent procurement management platform. It will let employees submit purchase requests, help reviewers manage approvals, and answer procurement policy questions using fictional documents.

The project is built in small, independently testable milestones, with maintainable code and explanations of important Go and gRPC concepts.

## Current status

**Phase 04: Go REST/JSON gateway.** A browser-facing HTTP gateway calls purchase and approval services over gRPC, with strict JSON validation, bounded bodies, CORS, timeouts, and controlled errors. The services support PostgreSQL or in-memory repositories. **Approval input is DEMO-ONLY: `manager_id` is not authenticated, and manager/comment metadata is not persisted.** Frontend, Docker setup, authentication, and AI remain future work.

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
- Planned: procurement dashboard and purchasing forms.
- Implemented: PostgreSQL persistence through pgxpool, isolated purchase/approval schemas, and versioned migrations.
- Planned: a policy assistant using fictional documents.

## Repository layout

```text
frontend/                    Vue application
api-gateway/                 Reserved Phase 00 directory
cmd/
  api-gateway/               Go REST/JSON entry point
  purchase-service/          Purchase server entry point
  approval-service/          Approval server entry point
  smoke/                     Create -> approve -> read demonstration
  migrate/                   Apply PostgreSQL migrations
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
```

Empty application directories contain `.gitkeep` placeholders so Git can track them. Remove a placeholder when its directory gains real files.

## Development preparation

The backend requires **Go 1.26 or newer** and PostgreSQL 14+ for persistent mode (verified with PostgreSQL 18.3). The in-memory demo does not require PostgreSQL. Generated Protocol Buffer code is included, so `protoc` is needed only when changing the contract. See [PostgreSQL setup and testing](docs/postgresql.md).

Use `.env.example` as the configuration reference. Servers read environment variables from the shell; they do **not** automatically load `.env`. The run guide shows PowerShell examples. Never commit `.env` files, credentials, or API keys.

Read [docs/architecture.md](docs/architecture.md) for the design. Local AI assistant guidance belongs in the ignored `AGENTS.md` file; keep additional agent and AI assistance documentation in the ignored `docs/agents/` and `docs/ai/` directories.

## Development roadmap

| Phase | Milestone | Expected verification |
| --- | --- | --- |
| 00 | Repository foundation and architecture documentation | Check directories, documentation, ignore rules, and Git diff |
| 01 | Original persistence-first plan superseded by the requested milestones | Persistence delivered in Phase 03 |
| 02 | Completed: runnable purchase and approval gRPC services with in-memory storage | Go unit tests, loopback gRPC integration tests, and executable smoke client |
| 03 | Completed: PostgreSQL persistence and service-owned schemas | Database constraint, migration, concurrency, and restart checks |
| 04 | Completed: Go REST/JSON gateway backed by gRPC | HTTP handler tests, live route checks, and Go build |
| 05 | Policy RAG assistant with pgvector and fictional documents | Retrieval tests and grounded-answer checks |
| 06 | Complete Docker Compose workflow and interview preparation | End-to-end smoke checks and documented demo walkthrough |

The requested Phase 04 gateway scope supersedes the original frontend milestone. Vue remains future work. Each milestone should remain small enough to review and verify independently.

## Data and Git workflow

Use fictional purchase records, suppliers, users, and policies. Work only on the existing `main` branch. Do not initialize Git again or automatically create branches, pull requests, commits, pushes, resets, or merges. Propose Conventional Commit messages for the user to apply manually.
