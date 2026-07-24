# Fidibeki Executable Retention Registry Implementation

> Source design:
> `docs/superpowers/specs/2026-07-24-executable-retention-registry-design.md`

## Durable Decisions

- Keep `GET /v1/privacy/retention` backward compatible and additive.
- Use a code-owned typed registry; this slice introduces no mutable policy and no migration.
- Never represent an unapproved maximum period as enforced.
- Every PostgreSQL table belongs to exactly one lifecycle category.
- Object-prefix, queue, browser-key, and provider boundaries are explicitly inventoried.
- Existing cleaners and expiry clocks remain unchanged.

## Tracer Bullet

Deliver one tenant-visible path from the typed registry through the existing API into the Data &
privacy backoffice:

1. Add lifecycle status, trigger, action, cadence, hold behavior, control reference, and resource
   coverage to the retention model.
2. Register all current PostgreSQL tables and known non-database storage boundaries.
3. Validate uniqueness, completeness, and enforcement claims; compare table coverage to migration
   DDL in tests.
4. Derive plan-specific attachment/replay durations and summary counts from the registry.
5. Render the four lifecycle states and operational detail in English and Portuguese.
6. Cover backward-compatible HTTP JSON and responsive desktop/mobile behavior.
7. Update the technical data map and main execution plan, then run full local regression.

## Acceptance

- [x] No category uses "Workspace lifetime" as an effective maximum period.
- [x] Every table created by current up migrations is covered exactly once.
- [x] Every maintained object-prefix, queue, browser-key, and provider family is represented.
- [x] `enforced`/`configurable` entries identify a real implementation and bounded lifecycle.
- [x] Pending legal decisions and external provider controls are visually and semantically explicit.
- [x] Existing API fields remain usable by old clients.
- [x] The registry cannot trigger deletion or change current cleaner behavior.
- [x] Backend, web, type/build, and focused desktop/mobile E2E checks pass locally.
- [x] No staging or production deployment is performed.
