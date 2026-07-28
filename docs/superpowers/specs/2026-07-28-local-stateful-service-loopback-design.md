# Local Stateful Service Loopback Design

**Issue:** [#153](https://github.com/fidibeki/app.fidibeki/issues/153)  
**Scope:** Local Docker Compose only  
**Decision:** Preserve the existing host ports and bind them explicitly to IPv4 loopback.

## Context

`infra/docker-compose.yml` publishes PostgreSQL, PgBouncer, Redis, and both
MinIO listeners without a host address. Docker therefore uses its unrestricted
publish default. These are development services with known local credentials;
they must not be reachable from another host on the developer's network.

The services still need host access for database tools, queue inspection, S3
uploads, and browser-facing local media. Removing every published port would
break those workflows, while an environment-configurable bind address would
make the insecure setting easy to restore accidentally.

## Design

Use the long Compose port syntax for every stateful listener:

- PostgreSQL: `127.0.0.1:5433` to container port `5432`
- PgBouncer: `127.0.0.1:6432` to container port `6432`
- Redis: `127.0.0.1:6380` to container port `6379`
- MinIO API: `127.0.0.1:9000` to container port `9000`
- MinIO console: `127.0.0.1:9001` to container port `9001`

Container-to-container traffic continues to use service DNS names and container
ports, so API, worker, migrations, health checks, and startup ordering do not
change. Host tools continue to use the same published port numbers.

IPv4 loopback is intentional. The current local URLs use `localhost`, and one
explicit mapping avoids platform-dependent dual-stack publication behavior.

## Regression Gate

Add a small Python standard-library checker that:

1. renders `infra/docker-compose.yml` through
   `docker compose config --format json`;
2. requires the four expected stateful services to remain represented in the
   policy;
3. allows a service with no published ports;
4. rejects every published port whose rendered `host_ip` is not exactly
   `127.0.0.1` or `::1`; and
5. reports the service and published/target ports without exposing environment
   values or credentials.

Unit tests exercise safe IPv4 and IPv6 loopback mappings, unpublished services,
missing policy services, omitted host addresses, wildcard IPv4/IPv6 addresses,
and mixed safe/unsafe mappings. Backend CI runs both the unit tests and the
checker against the real rendered Compose document. The workflow path filter
includes the checker and its tests so a future gate-only change is validated.

## Verification

- The checker fails against the current unrestricted configuration.
- Its unit tests pass.
- After the Compose change, rendered ports contain a loopback `host_ip`.
- `docker compose config --quiet` passes.
- PostgreSQL, PgBouncer, Redis, and MinIO become healthy in an isolated local
  Compose project.
- Each unchanged host port is reachable on `127.0.0.1`.
- Backend CI and Security CI pass on the integrated commit.

This change has no staging or production deployment surface.
