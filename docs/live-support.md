# Live support

Platform staff open **Conversations** in the backoffice, choose an online member,
and send a personal message. A member receives a dismissible preview in the app's
**Messages & feedback** button in the app header. Members can reply after staff start a thread,
including while staff are away. They cannot initiate a chat. Give feedback keeps
the existing structured feedback workflow.

## Access and data

- `platform:support:read` permits presence, inbox and conversation reads.
  `platform:support:write` permits initiating and sending messages. Grant both to
  staff who answer conversations. Read-only staff cannot send.
- Migration 138 grants both permissions to administrators who already hold all
  fourteen previous root permissions. Other administrators need an explicit
  grant. Refresh the staff session after granting permissions.
- Member endpoints derive the participant from the signed-in session. API keys,
  other members, other workspaces, suspended workspaces and pending deletion
  cannot access the thread. PostgreSQL additionally enforces participant RLS.
- Presence contains a workspace/member ID, an allowlisted section, active/idle
  state, and last heartbeat. It excludes URLs, query strings and page content.
  Tab keys expire in 60 seconds; the ID index expires in 120 seconds. Active
  sessions prune stale index entries. One person appears once across their tabs.
  The directory displays at most 500 recent browser sessions and reports truncation.
- Messages are plain text, limited to 4,000 Unicode characters. PostgreSQL
  serializes sequence allocation and rejects conflicting reuse of a send key.
  History and inbox pages contain at most 50 records. Read state is shared among
  platform staff and separately among a member's tabs.
- The daily worker deletes conversations 180 days after the last message,
  respecting active member privacy holds. Member/workspace deletion cascades.
  The retention health operation is `support_conversations`.
- Support transcripts are private to their participant and platform staff.
  Workspace and operator-managed member archives omit them. **Download
  conversation** uses the participant's authenticated, paged history and exports
  a complete JSON file at a fixed sequence cutoff. It fails without creating a
  partial file if access changes, a page is missing, 20 seconds elapse, or the
  output exceeds 20 MiB. Larger requests need staff-assisted privacy handling.

## Transport and operations

Member endpoints live under `/v1/support`: `GET /conversation?before=<sequence>`,
`POST /messages`, `POST /read`, `PUT /presence`, `DELETE /presence/<tab UUID>`,
and `GET /events`. Message writes contain `client_id` and `body`; reads contain
`sequence`; presence contains `tab_id`, `section` and `state`.

Staff endpoints live under `/admin/backoffice/support`: `GET /online`,
`GET /conversations?before=<conversation UUID>`, `GET /conversations/<UUID>`,
`GET /conversation?tenant_id=<UUID>&member_id=<UUID>`, `POST /messages`,
`POST /conversations/<UUID>/read`, and `GET /events`. Staff sends additionally
require `tenant_id` and `member_id`. Successful retries return the original
message; changed content using the same key returns 409.

Messages commit before a best-effort Redis notification. Streams carry only
invalidation events, never message bodies or credentials. Presence events refresh
only the online directory. Conversation events refresh the inbox and selected
thread. Reconnection reloads authoritative data, including after a Redis outage.

SSE uses the existing authenticated Nuxt proxies. Streams release their database
transaction before subscribing, close within 20 seconds or token expiry, and
revalidate permissions on every connection. Preserve `text/event-stream`,
`Cache-Control: no-store`, `X-Accel-Buffering: no`, and immediate flushing through
any production proxy. The normal API write timeout remains 30 seconds.

Observe HTTP failures on `/v1/support/*` and `/admin/backoffice/support/*`, Redis
availability, and retention health. A healthy browser shows **Live** in the
backoffice. Network interruption shows a reconnect state. Failed sends retain
the draft and reuse the send key on an unchanged retry. Messages saved during a
notification outage arrive on the next successful snapshot.

## Local verification

Requirements: Docker, the Go version in `backend/go.mod`, Node 24 or newer, pnpm
11.15.1, `psql`, and Chromium installed through Playwright.

```sh
pnpm install --frozen-lockfile
pnpm --filter @fidibeki/backoffice exec playwright install chromium
pnpm test:live-support
make coverage
make format-check lint governance-check
```

The browser script starts isolated PostgreSQL and Redis services and production
Go, app, widget and backoffice servers on loopback ports 54339, 6389, 8089,
4329, 4180 and 4179. It uses synthetic accounts and no external email delivery.
It checks staff initiation, member replies, durable reload, reconnect, multiple
tabs, transcript download, structured feedback submission, keyboard focus and
mobile overflow. It writes screenshots to `docs/evidence/live-support-*.png` and
keeps logs in its printed temporary directory. It stops its own services on exit.
Set `SUPPORT_SKIP_BUILD=1` only when all three frontend bundles are current.

## Release and rollback

1. Run the checks above and inspect the independent review report at
   `.agents/meta/review-chain-report.md`.
2. Apply migration 138 before deploying API and worker binaries. Start the worker
   with the new retention operation, and check its health after the first sweep.
3. Publish the widget assets before the app bundle. The new loader adds manual
   mounting for the shared launcher; ordinary customer embeds keep their behavior.
4. Deploy app and backoffice images. Both Docker builds include `shared/`.
   Preserve existing widget URL/key and proxy configuration. The feedback project
   must have an active rule targeting the application and its allowed domains.
5. Refresh a permitted staff session. With a test member in another browser,
   verify presence, a proactive message, reply, refresh, transcript download and
   Give feedback through the actual production proxies. Check another member
   cannot see the conversation and that read-only staff cannot send.

For an application rollback, roll back app/backoffice first, then API/worker,
leaving the additive tables and data in place. Older binaries ignore them. The
down migration deletes support conversations and messages and removes support
permissions; use it only when intentionally discarding that data after preserving
any required export. Do not run it as a routine application rollback. Broadened
audit permission and retention enum constraints remain compatible on rollback.

This change prepares the release. Production deployment and verification through
the production edge are separate release steps.
