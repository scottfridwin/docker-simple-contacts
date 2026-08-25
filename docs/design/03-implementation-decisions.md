# Contacts Project Implementation Decisions

## How to use this file
- Select one option for each decision item.
- Add clarifying notes where needed.
- Keep this file in the repository root so the coding agent can treat it as authoritative input.
- Once complete, include this file and the AI development guide in the initial implementation prompt.

## Decision Status
- Date: 08/25/2026
- Owner: Scott Fridlund
- Status: Final (with temporary defaults noted below)

## 1) Backend HTTP stack
Decision needed: API server and routing approach.

Options:
- Option A: Go standard library (net/http) + lightweight router
- Option B: Go framework (for example, Gin, Echo, Fiber)

Recommended default:
- Option A

Rationale:
- Minimal dependencies, easier long-term maintenance, better compatibility with strict linting and predictable behavior.

Selected option:
- **Option A**

Notes:
- 

## 2) Frontend framework
Decision needed: PWA frontend stack.

Options:
- Option A: React + Vite
- Option B: Vue + Vite
- Option C: SvelteKit

Recommended default:
- Option A

Rationale:
- Broadest ecosystem support, straightforward PWA plugins, mature testing and form libraries.

Selected option:
- **Option A**

Notes:
- 

## 3) Standard Person fields
Decision needed: exact standard fields and required/optional flags.

Recommended default field set:
- id (server-generated UUID, required)
- first_name (required)
- last_name (required)
- display_name (optional, derived fallback allowed)
- primary_email (optional)
- primary_phone (optional)
- notes (optional)
- created_at (server-generated, required)
- updated_at (server-generated, required)

Selected standard fields:
- id (server-generated UUID)
- first_name
- middle_names (optional ordered array of strings)
- last_name
- display_name (optional; derived fallback from name parts)
- custom_fields (JSONB)
- created_at (server-generated)
- updated_at (server-generated)

Required fields:
- id
- first_name
- last_name
- created_at
- updated_at

Notes:
- Start with just first_name and last_name as the user-settable fields. Expanding to include other useful fields can be a 1.0 goal AFTER we get a proof-of-concept running.
- Multiple middle names should be allowed, with the order being important. A composite "middle_name" value can be derived by concatenating the array of middle names in order.
- display_name is included to support default list sorting and can be auto-derived when not provided.

## 4) Custom fields policy
Decision needed: boundaries for custom field storage.

Options:
- Key format: lowercase snake_case only OR free-form labels
- Value types: string only OR string/number/boolean
- Per-person max field count
- Max key length
- Max string value length

Recommended default:
- Key format: lowercase snake_case
- Value types: string/number/boolean
- Max field count: 64
- Max key length: 64
- Max string value length: 1024

Selected policy:
- Key format: lowercase snake_case
- Value types: string/number/boolean/date
- Max field count: 64
- Max key length: 64
- Max string value length: 1024

Notes:
- Added "date" as a field type and kept all other defaults.

## 5) Delete behavior
Decision needed: hard delete vs soft delete.

Options:
- Option A: Hard delete (record removed permanently)
- Option B: Soft delete (deleted_at timestamp)

Recommended default:
- Option B

Rationale:
- Safer operational behavior and easier recovery from accidental deletion.

Selected option:
- **Option B**

Notes:
- Should include a "recycle bin" style delete mechanism so that deleted records are purged after some period of time.
- Temporary default for v1: purge soft-deleted records after 30 days.

## 6) Search and filtering depth in v1
Decision needed: list endpoint capabilities.

Options:
- Option A: Basic filtering on standard fields + sort + pagination
- Option B: Include full-text search on selected fields in v1

Recommended default:
- Option A

Rationale:
- Lower complexity for first release with clear migration path to full-text later.

Selected option:
- **Option A**

Notes:
- 

## 7) Pagination and sorting defaults
Decision needed: deterministic list behavior.

Recommended default:
- Default page size: 25
- Maximum page size: 100
- Default sort: updated_at desc

Selected defaults:
- Default page size: 25
- Maximum page size: 100
- Default sort: display_name desc

Notes:
- Display by name is the default for most contact management systems that I have used.

## 8) CORS policy
Decision needed: allowed origins for browser access.

Options:
- Option A: Explicit allow-list (recommended)
- Option B: Wildcard for development only

Recommended default:
- Explicit allow-list with separate development and production values.

Selected option:
- Agree with default.

Allowed origins (dev):
- http://localhost:5173
- http://127.0.0.1:5173

Allowed origins (prod):
- Set explicitly at deployment time to the reverse-proxy public origin(s).

## 9) License
Decision needed: repository license.

Options:
- Option A: MIT
- Option B: Apache-2.0

Recommended default:
- Option B

Rationale:
- Explicit patent grant and common choice for service-oriented projects.

Selected option:
- Temporary default for implementation: **Option B (Apache-2.0)**

Notes:
- I don't really care if anyone wants to copy or derive the work, only if they want to make money from it. Go with something standard for open-source projects.
- Important: Apache-2.0 permits commercial use. If non-commercial restrictions are required, replace before public release.

## 10) Versioning and release policy
Decision needed: branch and tag strategy.

Recommended default:
- Versioning: Semantic Versioning
- Release trigger: Git tag push in form vX.Y.Z
- Protected branch: main
- Image tags: vX.Y.Z, sha-<shortsha>, latest (main releases only)

Selected policy:
- Temporary default: use recommended policy in this section for v1.

Notes:
- Will be resolved at a later time
- For implementation now: Semantic Versioning, release on vX.Y.Z tags, protected main branch, image tags vX.Y.Z + sha-<shortsha> + latest (main releases only).

## 11) CI coverage threshold
Decision needed: enforce minimum line coverage or not.

Options:
- Option A: Enforce threshold in CI
- Option B: Report-only for first release

Recommended default:
- Option A with 70 percent minimum backend coverage initially.

Selected option:
- Agree with default.

Coverage threshold:
- Agree with default.

Notes:
- Whatever makes sense; it isn't a concern for me

## 12) Migration tooling
Decision needed: migration framework and execution model.

Options:
- Option A: golang-migrate
- Option B: goose

Recommended default:
- Option A

Rationale:
- Widely used, simple CLI and container-friendly workflows.

Selected option:
- **Option A (golang-migrate)**

Notes:
- Whatever makes sense; it isn't a concern for me
- Locked to Option A for implementation consistency.

## 13) PWA offline behavior
Decision needed: offline support target.

Options:
- Option A: No offline support in v1
- Option B: Read-only cache for last loaded list/detail
- Option C: Read/write queue with sync reconciliation

Recommended default:
- Option B

Rationale:
- Useful user experience gain without high synchronization complexity.

Selected option:
- **Option A**

Notes:
- For now let's not worry about offline access.

## 14) Logging format
Decision needed: runtime log format policy.

Options:
- Option A: JSON logs only in all environments
- Option B: JSON in production, human-readable in development

Recommended default:
- Option B

Selected option:
- Option B

Notes:
- When deployed, I want all logging to be routed to stdout/stderr so that it can be picked up by Docker log settings. No separate log files should be written; Docker controls the logging mechanism.

## 15) Initial performance target
Decision needed: explicit baseline for sizing and indexes.

Recommended default target:
- Dataset size: up to 100,000 Person records
- API list p95 latency: <= 300 ms for default page size
- API write p95 latency: <= 250 ms

Selected target:
- Recommended

Notes:
- If this is achievable that's great, but I am not too worried with performance initially. This is self-hosted and not intended to be enterprise-ready

## Final decisions summary (fill before implementation)
Copy completed values here so the coding agent has a compact, unambiguous input block.

- Backend HTTP stack: Option A (Go net/http + lightweight router)
- Frontend framework: Option A (React + Vite)
- Standard Person fields (required and optional): id (required), first_name (required), last_name (required), middle_names (optional ordered array), display_name (optional, derived fallback), custom_fields (optional JSONB), created_at (required), updated_at (required)
- Custom fields policy: snake_case keys; value types string/number/boolean/date; max 64 fields; max key length 64; max string length 1024
- Delete behavior: Soft delete (deleted_at) with recycle-bin behavior; temporary default purge after 30 days
- Search/filter scope: Option A (basic filtering + sort + pagination)
- Pagination/sort defaults: default page size 25; max page size 100; default sort display_name desc
- CORS policy: explicit allow-list; dev http://localhost:5173 and http://127.0.0.1:5173; prod set to reverse-proxy public origin(s)
- License: Temporary default Apache-2.0 (commercial use allowed); revisit if non-commercial restriction is required
- Versioning/release policy: Semantic Versioning; releases on vX.Y.Z tags; protected main; image tags vX.Y.Z + sha-<shortsha> + latest on main releases
- Coverage threshold: Enforce in CI; 70% minimum backend coverage
- Migration tooling: golang-migrate
- PWA offline behavior: Option A (no offline support in v1)
- Logging format: Option B (JSON in production, human-readable in development), all logs to stdout/stderr only
- Performance targets: up to 100,000 records; list p95 <= 300 ms; write p95 <= 250 ms (aspirational for v1)

## Handoff note for coding agent
Implement strictly according to:
- contacts-ai-development-guide.md
- contacts-implementation-decisions.md

If conflicts exist, values in this decisions file override recommendation defaults in the guide.
