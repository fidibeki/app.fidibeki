# Live support in Fidibeki

## Scope

Platform staff can see signed-in Fidibeki users who are active or idle, send a
personal message, and exchange messages immediately. This is first-party support
inside the tenant application, not a customer-installable chat product.

The app has one support launcher with Messages and Give feedback. Existing
structured feedback remains available. Incoming messages show a dismissible
preview and unread count without stealing focus. Conversations survive refresh,
navigation, temporary disconnects, and multiple tabs.

## Contract

- Presence is per browser tab, expires after 60 seconds, and reports an allowlisted
  app section rather than URLs, query strings, or page contents. Active and idle
  are distinct. Logout removes that tab; expiry handles crashes.
- Only platform staff can start a conversation. Users can reply to an existing
  thread even while staff are away, but cannot initiate one. The empty Messages
  view explains this, and Give feedback remains independently available.
- One conversation per tenant member. Members can access only their own thread;
  API keys cannot use support. Platform access has separate read and write
  permissions. Existing root administrators receive these permissions.
- PostgreSQL stores ordered messages, client-generated idempotency keys, and read
  positions. Messages have a 4,000-character limit. History and inboxes are paged.
- Redis stores ephemeral presence and broadcasts invalidations. Notifications
  contain no message content. Clients reload authorized data after connection
  and reconnect, so missed notifications cannot lose messages.
- Streams authenticate before opening, release database transactions, terminate
  before token expiry, and reconnect periodically to revalidate access. Writes
  are rate limited, audited, and isolated by participant and workspace.
- English and Portuguese app copy, keyboard access, narrow screens, loading,
  empty, offline, retry, and permission states are required.
- Conversation data participates in account deletion, export, and a documented
  retention policy. Presence is not session replay or an analytics identifier.

## Launch evidence

1. Migration up/down/up and constraints; real PostgreSQL and Redis integration.
2. HTTP authorization, tenant/member isolation, revoked access, idempotency,
   ordering, pagination, unread/read behavior, limits, and disconnect recovery.
3. Browser journey through real app and backoffice proxies: online user, proactive
   message, reply, refresh, reconnect, multiple tabs, and feedback entry point.
4. Responsive screenshots, frontend unit/type/build checks, backend checks, and
   regressions relevant to changed routes and middleware.
5. Independent security, correctness, and performance review, with findings
   resolved and a release/rollback runbook. Production deployment is separate.

## Implementation checklist

- [x] Durable schema and scoped API
- [x] Presence and streaming transport
- [x] Backoffice live users and inbox
- [x] App support launcher and feedback integration
- [x] Retention/export/deletion and operational documentation
- [x] Automated tests and full browser verification
- [x] Independent review and final launch audit

Final checks and responsive evidence: [launch validation](../../evidence/live-support-validation.md).
Release procedure: [live support runbook](../../live-support.md).
