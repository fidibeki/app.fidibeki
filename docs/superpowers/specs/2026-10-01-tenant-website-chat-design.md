# Tenant website conversations

This corrects the initial scope: the product customer is a Fidibeki tenant, and
its conversation participants are visitors to that tenant's website. Platform
support remains available as a separate existing feature.

A tenant opens Conversations in its dashboard, selects a project, enables chat,
sees anonymous online visitors, and sends a personal message. The existing embed
shows that message immediately and accepts replies. By default, visitors cannot
start a conversation. The tenant can enable **Allow visitors to start conversations**
for the selected project in Conversations. This independent flag defaults off,
updates open widgets in real time, and is enforced by the API. Visitors can then
leave a first message while the team is away. Turning it off blocks new visitor
starts and preserves replies in existing conversations. Messages and feedback
share one widget launcher. Chat is disabled per project until the tenant enables it.

The website uses the existing public key and allowed-origin boundary to request
an opaque, random visitor credential. The public key never authorizes transcript
access. Credentials are bound to the tenant, project, visitor and origin, stored
hashed server-side, and expire after 24 hours. The browser retains only this
functional chat credential, with its expiry, in project-scoped storage. A reset
API clears it on host logout or visitor request. Visitor names are anonymous,
not inferred from analytics identifiers, email addresses, or host account IDs.

Tenant members need explicit chat read/write/delete permissions. Workspace
administrators inherit these permissions; custom roles do not. Streams use short
independently authorized transactions and close before credential expiry. Redis
contains temporary, scoped presence and invalidations. PostgreSQL owns message
ordering, deduplication, history, read markers and tenant/project/visitor RLS.

Presence records contain a redacted current page, active/idle state and a
60-second expiry. Query strings and fragments are excluded. Chat content is
blocked from session replay. Configuration changes and credential revocation
stop public chat access. Deleting a project or workspace cascades its chat data.
Inactive conversation retention is 180 days; unused expired sessions are removed
on the maintenance sweep. Active tenant privacy holds prevent conversation
retention deletion. No conversation content enters analytics or diagnostic logs.

Acceptance covers two tenant projects and independent visitor credentials,
operator initiation, flag-controlled visitor initiation, partial settings updates,
failed saves, preserved drafts, existing replies after flag-off, live settings
changes, visitor reply, live presence, multiple tabs, unread state,
reconnection, durable history, transcript download, existing feedback submission,
permission revocation, origin isolation, rate limits, and retention. Browser QA
must exercise the tenant dashboard and a separate website embed against the real
API, PostgreSQL and Redis, on desktop and mobile.

Final requirement-by-requirement evidence: [launch audit](../../evidence/website-chat-launch-audit.md).
