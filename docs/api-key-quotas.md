# API key quotas

`rate_limit_per_min` is enforced for every authenticated tenant API-key request. The quota includes
reads, writes, downloads, range requests, and `HEAD` probes. Invalid, expired, deleted, and
cross-tenant credentials are rejected before a quota is charged; a valid credential without the
required permission is charged because authentication succeeded.

The API uses the immutable internal key ID in Redis (`tenant-api-key:<id>`), never the raw token,
so the quota is shared by all API instances. Redis failures fail closed with `503` and
`Retry-After: 1`.

Successful and rate-limited API-key responses include `X-RateLimit-Limit` and
`X-RateLimit-Remaining`. Exhausted quotas return `429` with `Retry-After`.
