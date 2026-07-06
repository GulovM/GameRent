# RentGameAccs

> Multi-agent orchestration framework for agentic coding

## Project overview

GameRent is a full-stack platform for renting gaming accounts.

Primary stack:

* Backend: Go
* Database: PostgreSQL
* Cache and runtime coordination: Redis
* Frontend: React + TypeScript + Vite
* Reverse proxy: Nginx
* Local infrastructure: Docker Compose
* Database migrations: Goose

Repository structure:

```text
cmd/            application entrypoints
internal/       backend domain and infrastructure code
migrations/     Goose SQL migrations
test/           backend and integration tests
web/            React/Vite frontend
docs/           architecture and product documentation
```

Do not assume this is a Node.js backend project.

`npm install`, `npm run build` and `npm test` apply only to the frontend under `web/`, not to the whole repository.

## Documentation hierarchy

Before non-trivial work, inspect the relevant files in `docs/`:

* `01-vision.md` — product boundaries and MVP scope.
* `02-srs.md` — functional and non-functional requirements.
* `03-security.md` — security model and threats.
* `04-database-design.md` — schema rationale and database rules.
* `05-api-design.md` — API contracts.
* `06-architecture.md` — backend and frontend architecture.
* `07-domain-model.md` — domain entities, states and invariants.
* `08-project-structure.md` — package layout.
* `09-deployment.md` — infrastructure and deployment.
* `10-sequence-diagrams.md` — critical business flows.
* `11-development-roadmap.md` — implementation priorities.

Documentation defines intended behavior.
Existing code, migrations and tests define the current executable behavior.

When documentation and implementation conflict, identify the conflict explicitly before changing behavior.

## Current domain boundaries

Typical backend modules:

* `internal/auth` — registration, login, JWT, refresh token rotation, RBAC.
* `internal/user` — user profiles and user management.
* `internal/account` — Steam account lifecycle and encrypted credentials.
* `internal/game` — game catalog and Steam library metadata.
* `internal/rental` — rental lifecycle, account availability and duration logic.
* `internal/payment` — payments, deposits and webhook processing.
* `internal/shared` — configuration, logging, middleware, errors, DB and Redis.
* `internal/repository/postgres` — PostgreSQL persistence.
* `web/` — React application and typed API client.

Prefer focused changes inside the affected domain module.

Do not introduce a new framework, ORM, message broker, event bus, package layout or architectural layer unless the task clearly requires it.

## Mandatory domain invariants

### Rental and account availability

* One gaming account must never have more than one active rental.
* Creation of a rental must be atomic.
* Use PostgreSQL transactions for operations changing rental, account, payment or balance state together.
* Use row locking or existing database constraints for competing attempts to rent the same account.
* Never rely only on an application-level availability check for concurrency-sensitive operations.
* Preserve valid account and rental state transitions.
* Expired rentals must not expose credentials or remain active.

### Payments and balance

* Monetary values must use integer minor units.
* Never use `float32` or `float64` for price, deposit, balance or payment amount.
* Balance updates, payment confirmation, rental activation, cancellation, refund and deposit release must be atomic.
* Payment webhook processing must be idempotent.
* Repeated delivery of one webhook must not create duplicate payments, rentals, charges or refunds.
* Do not allow negative balances unless credit functionality is explicitly introduced.

### Security

* Never log passwords, Steam credentials, JWTs, refresh tokens, encryption keys, webhook secrets or raw Authorization headers.
* Steam passwords must remain encrypted at rest.
* Standard account endpoints must not expose Steam credentials.
* Refresh tokens must be persisted only as hashes.
* `ADMIN` is required for administrative actions.
* `RENT` is the normal customer role.
* Credentials may be returned only to the owner of an active, paid and unexpired rental.
* Do not weaken RBAC, rate limits, validation, encryption or audit logging to simplify implementation.
* Security-sensitive state changes should create audit or security events when supported by existing modules.

### API and database compatibility

* Preserve existing API response envelopes and error formats.
* Do not silently rename or remove public routes, response fields, enum values or query parameters.
* Every schema change requires a new Goose migration.
* Never modify an already-applied migration.
* Every migration must contain both `-- +goose Up` and `-- +goose Down`.
* Never run destructive database cleanup commands unless explicitly requested.

## Ruflo execution model

Ruflo coordinates memory, task decomposition and review context.

Codex performs the actual work:

* reads repository files;
* changes source code;
* creates migrations;
* runs tests and commands;
* validates results.

Critical rule:

> Do not stop after Ruflo MCP calls. Continue with implementation, validation and review.

Use Ruflo memory before complex work, but do not save secrets or noisy one-off details.

## When to use swarm coordination

Use a swarm only for work that crosses domains or has elevated risk:

* changes to rentals, payments, balances or account availability;
* database migrations affecting core entities;
* security-sensitive changes;
* changes spanning backend and frontend;
* API contract changes;
* multi-module refactoring;
* performance or concurrency work.

Do not use a swarm for:

* typos;
* markdown edits;
* one-file formatting fixes;
* simple isolated UI text changes;
* trivial bug fixes with no domain impact.

Default swarm size: maximum 4 roles.

Do not start 8 agents by default.

## Swarm configurations

### Standard backend feature

Use:

1. `architect`

   * Reviews affected modules, API contract, migrations and invariants.
   * Produces a concise plan before implementation.

2. `go-backend-engineer`

   * Implements handlers, services, repositories, migrations and tests.
   * Preserves existing conventions.

3. `tester`

   * Defines edge cases and validates the implementation.

4. `reviewer`

   * Reviews regression risk, API compatibility and code quality.

### Rentals, payments, balance or concurrency

Use:

1. `domain-architect`

   * Owns rental states, payment states, account availability and user-visible behavior.

2. `postgres-concurrency-engineer`

   * Reviews transactions, row locks, indexes, uniqueness guarantees and idempotency.

3. `go-backend-engineer`

   * Implements the approved design.

4. `security-reviewer`

   * Checks authorization, credential exposure, webhook verification and secret handling.

### Backend + frontend feature

Use:

1. `api-architect`

   * Defines or validates the API contract.

2. `go-backend-engineer`

   * Implements backend, repository and migration changes.

3. `react-engineer`

   * Updates API client, UI state, validation and error handling in `web/`.

4. `reviewer`

   * Verifies cross-layer compatibility and regression risk.

## Required workflow for non-trivial tasks

1. Search Ruflo memory using task-specific keywords.
2. Read relevant files from `docs/`.
3. Inspect affected code, migrations, tests and frontend calls.
4. Identify domain invariants and compatibility constraints.
5. Initialize a small hierarchical swarm only when justified.
6. Create a concise implementation plan.
7. Implement the change.
8. Add or update tests.
9. Run relevant validation commands.
10. Review the final diff for secrets, race conditions, broken contracts and migration safety.
11. Store only reusable, non-sensitive conclusions in Ruflo memory.

## Ruflo memory policy

Use these namespaces:

* `gamerent/architecture`
* `gamerent/domain-invariants`
* `gamerent/api-contracts`
* `gamerent/database-patterns`
* `gamerent/security`
* `gamerent/decisions`
* `gamerent/bugs`

Store:

* confirmed architectural decisions;
* transaction and locking patterns;
* API compatibility rules;
* verified bug causes and fixes;
* domain invariants;
* useful test setup details;
* deployment constraints.

Never store:

* `.env` content;
* passwords;
* Steam credentials;
* JWTs;
* refresh tokens;
* encryption keys;
* payment webhook secrets;
* database credentials;
* user personal data.

## Validation commands

Run the narrowest relevant checks first.

Backend formatting:

```powershell
make fmt
```

Backend tests:

```powershell
make test
```

Integration tests with PostgreSQL and Redis:

```powershell
make test-integration
```

Go static analysis:

```powershell
go vet ./...
```

Frontend dependencies:

```powershell
make web-deps
```

Frontend build:

```powershell
make build-web
```

Full application build:

```powershell
make build
```

Local infrastructure:

```powershell
make infra-up
```

Full Docker stack:

```powershell
make up
make ps
```

Health checks:

```powershell
Invoke-WebRequest http://localhost:8080/healthz
Invoke-WebRequest http://localhost:8080/health/ready
Invoke-WebRequest http://localhost:5173/healthz
```

Do not run `make tidy` unless dependencies were intentionally changed.

## Completion requirements

After every implementation task, report:

1. What changed.
2. Which files changed.
3. Which domain invariants were preserved or added.
4. Which commands were run and their actual results.
5. Any limitation, assumption or manual verification still required.

Do not claim that tests passed unless they were actually executed successfully.
