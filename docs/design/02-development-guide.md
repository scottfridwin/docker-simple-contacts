# Contacts Project AI Development Guide

## Purpose
This document is the implementation guide for the coding agent that will build the Contacts project. It translates the design goals into explicit engineering requirements, quality gates, and delivery criteria.

## Source Context
This guide accompanies the project design spec and should be treated as the primary execution contract for implementation details.

## Product Goal
Build a self-hosted contact management system with:
- A Go backend exposing a RESTful API.
- A PostgreSQL backend database running in a separate container.
- A PWA frontend for create/read/update/delete (CRUD) operations on contact records.
- Container image output as the primary deployment artifact.

## Scope for v1
### In scope
- Single entity domain: Person.
- Standard fields plus user-defined custom fields.
- REST API for CRUD.
- PWA web UI for CRUD.
- Containerized deployment.
- Developer experience in VS Code dev container.
- CI workflows for lint, test, build, and release.

### Out of scope
- Built-in authentication and authorization.
- SSO integration (including Authentik).
- Multi-tenant support.
- Contact sync integrations (Google, Outlook, LDAP, etc.).
- Background jobs/queues.
- File attachments.

## Required Technology and Project Layout
### Backend
- Language: Go (current stable major release).
- HTTP API framework: choose one lightweight framework or standard library and keep dependencies minimal.
- Data access: PostgreSQL with parameterized queries.
- Migrations: required and committed in repository.

### Frontend
- PWA-capable web frontend.
- Must support responsive layouts for desktop and mobile.
- Must support managing both standard fields and custom fields.

### Infrastructure
- Dockerfiles for backend and frontend (or single combined image only if explicitly justified).
- Docker Compose for local development with at least:
  - app service
  - postgres service
- Dev container configuration for VS Code.

### Repository baseline files
- README.md
- LICENSE
- .gitignore
- .editorconfig
- renovate.json (or equivalent Renovate config)
- CI workflow files under .github/workflows

## Person Data Model (v1 requirements)
The Person record must support:
- id (stable unique identifier)
- Standard fields (minimum expected): first_name, last_name, display_name, email, phone, notes
- custom_fields for user-defined values
- created_at and updated_at timestamps

### Custom fields
- Stored in PostgreSQL JSONB.
- Keys must be strings.
- Values may be string/number/boolean.
- Null handling rules must be defined and consistent across API and UI.

## API Requirements
### General
- Versioned API path prefix: /api/v1.
- JSON request and response bodies.
- Standardized error response schema.
- Health endpoint and readiness endpoint.

### Required endpoints
- POST /api/v1/persons
- GET /api/v1/persons
- GET /api/v1/persons/{id}
- PATCH /api/v1/persons/{id}
- DELETE /api/v1/persons/{id}

### List behavior
- Pagination required.
- Sort required on at least one deterministic field.
- Filtering support for selected standard fields.

### Validation
- Input validation required on all write operations.
- Reject unknown malformed payloads with clear error messages.
- Ensure all SQL interaction is injection-safe.

### API contract artifact
- OpenAPI specification must be authored and committed.
- CI should validate that implementation aligns with OpenAPI contract.

## Configuration and Secrets
The backend must support configuration through environment variables:
- DB_HOST
- DB_PORT
- DB_USER
- DB_PASSWORD
- DB_PASSWORD_FILE
- DB_NAME (default: postgres)
- PORT
- LOG_LEVEL

### Secret precedence rule
- If DB_PASSWORD and DB_PASSWORD_FILE are both present, DB_PASSWORD_FILE should take precedence.
- If neither is present, startup fails with explicit error.

## Security and Trust Model
- Application assumes upstream reverse proxy handles authentication and authorization.
- Application still must enforce:
  - strict input validation
  - secure defaults for HTTP headers where applicable
  - safe logging without secret leakage
- No auth middleware is required in v1.

## Observability and Operations
- Structured logging required.
- Include request correlation identifier support.
- Health endpoint for process liveness.
- Readiness endpoint dependent on database connectivity.
- Graceful shutdown behavior on termination signals.

## Data Lifecycle
- Migration strategy must be forward-only for v1.
- Startup should fail clearly if migration state is invalid.
- Deletion strategy (hard delete or soft delete) must be implemented consistently and documented.

## Testing Requirements
### Backend tests
- Unit tests for validation and core business logic.
- Integration tests against PostgreSQL (containerized test environment).
- Endpoint-level tests for CRUD behavior and error paths.

### Frontend tests
- Component/form validation tests.
- Basic end-to-end flow test for create and edit Person records.

### Quality thresholds
- Linting is mandatory.
- Test execution is mandatory in CI.
- Any failing lint/test/build step blocks merge.

## Linting and Formatting
- Strict linting rules must be configured and enforced in CI.
- Language-specific formatters must run consistently in local and CI paths.
- The repository should document developer commands for lint, format, and test.

## CI/CD and Release Management
### Pull request checks
- Lint
- Tests
- Build
- Migration check
- Container build validation

### Release flow
- Semantic versioning.
- Build and publish container image on version tags.
- Include image tags for:
  - semantic version
  - commit SHA
  - latest (only on approved branch)

## Containerization Requirements
- Application runtime must be deployable as container image.
- No standalone host installation path required in v1.
- Image should run as non-root user where possible.
- Healthcheck instruction in Dockerfile or Compose is required.

## Developer Experience Requirements
- Dev container should include:
  - Go toolchain
  - frontend toolchain
  - linters
  - test tools
  - migration tooling
- One command path for local bootstrap should be documented in README.

## Documentation Requirements
README must include:
- Project overview
- Architecture summary
- Quickstart for local development
- Configuration reference
- Build, test, lint commands
- API docs location
- Release process summary

## Acceptance Criteria for v1
1. A developer can clone the repository, open in dev container, run bootstrap commands, and start the full stack locally.
2. CRUD operations for Person work through both API and PWA.
3. Custom fields can be created, edited, persisted, and retrieved reliably.
4. CI passes lint, tests, migrations, and image build checks.
5. Tagged releases produce versioned container images.
6. Runtime config supports DB secret via direct value or file path.

## Explicit Decisions Required Before Implementation
The project owner must confirm the following items before coding starts:

1. Backend HTTP approach
- Decision needed: standard net/http vs specific framework.
- Why needed: affects routing, middleware, error handling, and testing patterns.

2. Frontend framework choice
- Decision needed: React, Vue, Svelte, or another stack.
- Why needed: determines build tooling, PWA setup, and test ecosystem.

3. Person standard field schema (final)
- Decision needed: exact list of standard fields and required vs optional flags.
- Why needed: impacts API contract, DB schema, and UI form rules.

4. Custom field constraints
- Decision needed: max number of custom fields, max key/value lengths, allowed value types.
- Why needed: prevents unbounded schema growth and inconsistent data.

5. Delete behavior
- Decision needed: hard delete or soft delete.
- Why needed: affects schema, API semantics, and filtering behavior.

6. Search/filter depth for v1
- Decision needed: basic filtering only vs full-text search.
- Why needed: impacts indexes, query complexity, and performance requirements.

7. API pagination and sorting defaults
- Decision needed: default page size, max page size, default sort order.
- Why needed: ensures deterministic list behavior and avoids performance regressions.

8. CORS policy
- Decision needed: allowed origins in local and production deployment.
- Why needed: required for secure browser access patterns.

9. License selection
- Decision needed: exact license (for example MIT or Apache-2.0).
- Why needed: needed for repository legal baseline.

10. Versioning and branching policy
- Decision needed: release branch model and tag trigger rules.
- Why needed: required to implement GitHub workflows correctly.

11. Coverage threshold policy
- Decision needed: whether to enforce minimum test coverage percent in CI.
- Why needed: influences gate strictness and implementation effort.

12. Database migration tooling
- Decision needed: specific migration tool.
- Why needed: controls migration file format, runtime execution, and CI verification.

13. Offline behavior for PWA
- Decision needed: no offline support, read-only offline cache, or read/write queueing.
- Why needed: major impact on frontend architecture and complexity.

14. Logging format standard
- Decision needed: plain structured JSON only vs environment-dependent formatting.
- Why needed: impacts observability tooling compatibility.

15. Initial performance target
- Decision needed: expected record counts and acceptable API latency target.
- Why needed: determines index strategy and early optimization priorities.

## Handoff Instructions for Coding Agent
- Build in small, reviewable increments.
- Keep all interfaces and behavior aligned with this guide.
- Do not introduce v2 features unless explicitly approved.
- Update README and OpenAPI whenever behavior changes.
- Treat CI failures as blockers.
- Prefer simple, maintainable implementations over premature optimization.
