# Website conversations

Each Fidibeki workspace can see visitors on its own website and start conversations with them. Open **Conversations** in the tenant dashboard, select a website project, and choose **Enable website chat**. Chat is off by default. The existing Fidibeki website embed picks up the setting; reload the website after enabling it.

The Online list shows anonymous visitors, active/idle status, and a privacy-filtered page location. Choose a visitor and send a message. It appears immediately in the website's **Messages & feedback** widget, with a preview the visitor can open or dismiss. By default, visitors can reply only after your team sends the first message. Turn on **Allow visitors to start conversations** in Conversations for the selected website project to accept first messages, including while your team is away. The switch is off by default for every project. Turning it off blocks new visitor-started chats and keeps existing conversations open. The permission is enforced by the API and updates open widgets in real time. Feedback stays in a separate tab of the same widget and submits through the existing feedback form.

Conversations remain in Inbox after visitors leave. Team members can read older messages, see unread replies, download a complete JSON transcript, and delete a conversation if they have permission. Sending is disabled once the visitor credential expires. Reconnecting or reopening the page recovers persisted messages and does not resend a successfully accepted message.

## Access and identity

- `project:read` and `chat:read` allow access to Conversations.
- `chat:write` allows sending messages. `project:update` controls both project chat settings.
- `chat:delete` allows deleting a conversation, with confirmation. Active tenant privacy holds block deletion.
- Existing full administrators inherit the three new permissions. Custom roles retain their existing permissions; an administrator can grant chat access explicitly.
- Visitors have anonymous project- and origin-bound credentials, valid for 24 hours. Multiple tabs share the browser identity. The widget does not infer a name or email from a website login.

A website that supports login/logout or account switching should reset chat at that boundary:

```js
await window.Fidibeki.chat.reset()
```

Wait for `fidibeki:ready` if the embed has not initialized. Reset also works before the chat panel mounts and while chat is turned off. It immediately forgets the previous browser credential, clears the visible history and drafts, and attempts to revoke the credential on the server. Visitors can do the same with **Forget this chat on this browser**. Reset does not erase the tenant's conversation record; use the dashboard's deletion action for that.

The browser stores only the opaque credential, not message transcripts. Website owners should include the feature in their own disclosures and integrate logout reset where relevant. The manual embed mode (`data-fidibeki-manual="true"`) continues to delegate the launcher to its host application; normal website embeds show the chat launcher.

## Appearance and mobile placement

Messages and feedback share one panel and the matching feedback rule's colors, logo, position and theme. The feedback panel sizes itself to the complete form, grows when fields expand, and shrinks to the submission confirmation. The launcher is hidden while feedback is open so the panel can use that space. There is no separate scrolling area inside the feedback form; if the complete panel exceeds the visible screen, the panel itself scrolls to keep every control reachable. The chat reset action appears in Messages.

On screens up to 600px wide, the launcher becomes a compact chat button and leaves 64px of space for a website's bottom navigation, plus the device safe area. Opening the panel hides the launcher; the panel follows the visible viewport when the keyboard opens. Sites with a different navigation height can set the additional bottom offset on their page:

```css
/* Use your bottom navigation height; use 0px if the page has none. */
@media (max-width: 600px) {
  :root { --fidibeki-chat-bottom-offset: 80px; }
}
```

The offset is added to the normal edge margin and mobile safe area. The desktop default is 0px.

## Data boundaries

Tenant/project/visitor scope is enforced by API authorization and PostgreSQL row-level security. A public project key alone cannot read a conversation or reply. Server-sent events carry invalidations, and authenticated requests fetch the actual content. Member permission, two-factor, tenant status, origin and visitor expiry checks apply on reconnect. API keys cannot act as human chat operators.

Presence expires after 60 seconds without a heartbeat; its Redis index expires after 120 seconds. Current page locations omit query strings and fragments and use the existing project targeting privacy projection; unmatched pages show only the origin. Chat content is blocked from the session replay recorder.

Message history is retained for 180 days after the last message. Unused visitor records become eligible for deletion 24 hours after credential expiry. Cleanup runs daily, continues full batches promptly, cascades to message rows, and records retention health. Any active tenant legal hold preserves anonymous conversation records because they cannot be reliably matched to a named privacy case.

## Operations and tests

Deploy migrations through 140 with the API before updating widget assets and the tenant web application. Migration 140 adds the visitor-initiation flag with a default of false. Disable chat per project to stop new website conversations; authorized team members can still read retained history. Platform backoffice support is a separate feature and uses separate tables and permissions.

`pnpm test:live-support` runs an isolated PostgreSQL/Redis/API/widget/web/backoffice stack. Its website-chat journey uses a tenant dashboard and a separate storefront origin, checks live delivery and replies, reconnect recovery, multi-tab presence, transcript download, feedback submission and reset. It also enables visitor initiation from the dashboard, sends a visitor's first message into Inbox, and verifies that disabling the flag preserves existing replies while blocking new conversations. It never connects to production. Backend integration tests cover isolation, permissions, expiry, pagination, retention and legal holds. Browser regression tests cover reset before mounting, feedback after disabling chat, inbox refresh, switching while older history is loading, per-project settings, failed saves and preserved drafts when the flag changes before sending.
