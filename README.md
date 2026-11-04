# ProcureFlow AI

ProcureFlow AI is an interview-ready portfolio project for an intelligent procurement management platform. It will let employees submit purchase requests, help reviewers manage approvals, and answer procurement policy questions using fictional documents.

The project is built in small, independently testable milestones, with maintainable code and explanations of important Go and gRPC concepts.

## Current status

**Phase 00: Repository Foundation.** The repository contains the initial directory structure, architecture documentation, contribution guidance, and a configuration template. Application services, database infrastructure, and tests have not been implemented yet. There is no runnable application at this stage.

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

## Planned functionality

- Create, view, and manage purchase requests with validation and clear status tracking.
- Review purchase requests through an approval workflow.
- Display procurement activity in a dashboard and provide purchasing forms.
- Expose a REST/JSON API for the frontend through a Go gateway.
- Answer policy questions using retrieval from fictional procurement documents, with references to supporting content.

## Repository layout

```text
frontend/                    Vue application
api-gateway/                 Go REST/JSON entry point
services/
  purchase-service/          Purchase request ownership and business rules
  approval-service/          Approval workflow ownership and business rules
proto/                       Protocol Buffer service and message definitions
database/
  migrations/               Versioned PostgreSQL schema changes
docs/
  architecture.md           System design and service boundaries
```

Empty application directories contain `.gitkeep` placeholders so Git can track them. Remove a placeholder when its directory gains real files.

## Development preparation

The planned development toolchain includes Go, Node.js with npm, Docker with Docker Compose, and the Protocol Buffers compiler (`protoc`). Go code generation will also require the Go and gRPC plugins for `protoc`. Versions and setup commands will be documented when the runnable foundation is introduced.

Use `.env.example` as a reference for future local configuration. Copy it to `.env` when configuring the application and replace the placeholder password with a local-only value. These variables are proposed configuration names; no application currently reads them. Never commit `.env` files, credentials, or API keys.

Read [docs/architecture.md](docs/architecture.md) for the design. Local AI assistant guidance belongs in the ignored `AGENTS.md` file; keep additional agent and AI assistance documentation in the ignored `docs/agents/` and `docs/ai/` directories.

## Development roadmap

| Phase | Milestone | Expected verification |
| --- | --- | --- |
| 00 | Repository foundation and architecture documentation | Check directories, documentation, ignore rules, and Git diff |
| 01 | Purchase service foundation: Go module, PostgreSQL migration, gRPC contract, and create/get request operations | Go unit tests, PostgreSQL integration tests, and gRPC smoke check |
| 02 | Approval service and workflow transitions | Workflow unit tests and service integration tests |
| 03 | Go API gateway with REST/JSON endpoints | HTTP validation and gRPC error-mapping tests |
| 04 | Vue dashboard and purchasing forms | TypeScript checks, production build, and manual flow verification |
| 05 | Policy RAG assistant with pgvector and fictional documents | Retrieval tests and grounded-answer checks |
| 06 | Complete Docker Compose workflow and interview preparation | End-to-end smoke checks and documented demo walkthrough |

Introduce local database infrastructure as needed in Phase 01, then extend it as services are added. Each milestone should remain small enough to review and verify independently.

## Data and Git workflow

Use fictional purchase records, suppliers, users, and policies. Work only on the existing `main` branch. Do not initialize Git again or automatically create branches, pull requests, commits, pushes, resets, or merges. Propose Conventional Commit messages for the user to apply manually.
