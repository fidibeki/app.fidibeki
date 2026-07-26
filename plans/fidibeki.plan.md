# Plan: Fidibeki

> Source PRD: Conversation with founder — instant feedback capture SaaS for web apps.
> This plan was stress-tested via a 41-branch grilling interview. The product decisions are
> locked; implementation details may be tightened when verification exposes an unsafe or
> internally inconsistent contract.

## Product direction override (2026-07-22)

This section is the current product goal and overrides earlier MVP decisions where they conflict.
In particular, feedback no longer auto-creates an issue, reporter text is a description rather than
the title, and opening the widget is not a reason to suppress it.

### Goal and environment boundary

Complete the operating loop:

`capture feedback -> analyze and triage -> decide action -> track resolution -> communicate outcome`

- Staging is sufficient while this loop is being completed.
- Do not perform production, deployment, or infrastructure work unless it directly blocks use of
  the application in staging.
- Production work resumes only with explicit founder authorization.
- On 2026-07-24, the founder explicitly authorized deploying the already validated product work to
  staging so its authenticated gates can run and completed issues can close. This authorization is
  staging-only and does not authorize production promotion or new infrastructure.
- Deliver product work as testable vertical slices. Each slice includes compatible migrations,
  backend, backoffice, and widget tests where applicable, plus staging E2E validation.

### Current execution TODO (approved 2026-07-22)

The canonical product design is
`docs/superpowers/specs/2026-07-22-fidibeki-three-pillar-product-roadmap-design.md` at commit
`6f9a029` or later. It supersedes the older slice order below where they conflict, without removing
completed work or compatibility guarantees. Audit each phase against code and staging before
implementation; extend existing contracts instead of rebuilding them.

#### Phase 0: product quality gate

- [x] [#8](https://github.com/acidiney/app.fidibeki/issues/8) Verified member domains. The audit
  found no workspace-domain verification model or invite/activation enforcement. Add the smallest
  tenant-scoped model and management surface that can prove a domain, then enforce it both when an
  invitation is created and when its magic link is consumed. Preserve workspaces without verified
  domains until they opt into the policy. Implemented with tenant-scoped RLS storage, hashed
  one-time email verification, serialized invite/activation enforcement, Settings management, and
  backend/web regression coverage. Migration `000053` and preserved tenant data were verified in
  staging.
- [x] [#9](https://github.com/acidiney/app.fidibeki/issues/9) Feedback queue selection. Filtering,
  URL synchronization, and SPA navigation already work. Replace path-only router active matching
  with one canonical queue matcher so exactly one queue reflects the current filter state.
  Implemented with a pure query classifier, explicit `aria-current`, and unit/browser regression
  coverage for default, special, archived, and ambiguous filter combinations.
- [x] [#10](https://github.com/acidiney/app.fidibeki/issues/10) Responsive session replay. Replay
  loading and playback already work. Constrain and scale the recorded viewport plus controls inside
  the available surface, with no page-level horizontal overflow on desktop, tablet, or mobile.
  Implemented with content-width observation, proportional viewport sizing, in-place player updates
  that preserve playback state, and browser geometry coverage at 1440, 1024, and 390 pixels.
- [x] [#11](https://github.com/acidiney/app.fidibeki/issues/11) Magic-link confirmation alignment.
  Preserve the existing accessible status message while giving its icon and wrapping copy a stable
  aligned layout at narrow and wide widths. Implemented as an explicit icon/copy grid that retains
  `role="status"`, with geometric browser assertions in the desktop and mobile login flows.
- [x] [#12](https://github.com/acidiney/app.fidibeki/issues/12) Unknown workspace routing. The API
  already returns `unknown_tenant`; the web login route currently bypasses that knowledge. Resolve
  workspace availability before authentication, render a neutral unavailable/not-found state,
  offer editable prefilled signup only for standard Fidibeki subdomains, and never derive a slug
  from an unknown custom domain. Implemented by reusing the public tenant configuration endpoint,
  mapping inaccessible tenant states to one neutral SSR 404, preserving temporary-error retry, and
  carrying a validated direct-subdomain slug only through an explicit signup action.

Phase 0 exit gate passed on 2026-07-22: all backend, web, security, and release checks were green;
API, worker, and web were promoted only to staging at commit `655f3b4`; the database was clean at
migration `000053`; and the real staging Playwright smoke passed against an existing tenant. The
unknown-workspace journey returned a neutral SSR `404` with an explicit editable-slug signup action,
while the existing tenant login and service health checks remained available. No production
environment was changed.

#### Phase 1: Contextual Feedback Core

- [x] Re-audit widget configuration, public runtime contracts, targeting, recurrence, analytics,
  submission, and inbox behavior as complete/partial/missing.
  - Complete and preserved: independent Feedback creation; editable Feedback title/description;
    attachment declaration/upload/completion; exact, prefix, and safe-regex include/exclude
    targeting plus URL testing; canonical query/hash/trailing-slash handling; durable recurrence;
    funnel boundaries and dashboard; automatic script installation; inbox visibility.
  - Partial and extended in place: `widget_rules` already stores the configuration aggregate and
    public endpoint, but only exposes legacy capability booleans, one greeting, one fixed form,
    trigger timing, and limited theme tokens. Reporter contact/consent exists in the submission
    backend but has no widget UI. Browser state falls back to eligibility rather than an in-memory
    page-lifetime state, and its storage key omits the origin.
  - Missing: a stable definition key and revision, typed presentation/objective/response blocks,
    locale variants and project default locale, declarative and imperative mounts, launcher,
    inline, reaction, and guarded prompt views, and presentation-specific fatigue policy.
  - Implementation path: evolve `widget_rules` additively as the persisted `WidgetDefinition`, keep
    legacy fields and response keys decodable, expose a versioned public definition contract, then
    route automatic/declarative/imperative placement through one runtime before completing the four
    presentations and consented contact end to end.
- [x] Add one versioned `WidgetDefinition` shared by `launcher`, `inline`, `reaction`, and `prompt`,
  including lifecycle, placement, objective, supported response blocks, locale variants, controlled
  theme tokens, and presentation-specific fatigue guardrails. Migrations `000054` and `000055`
  preserve existing rules and feedback while adding definition revisions, lifecycle, presentations,
  objectives, structured response blocks, locale variants, project default locale, definition-version
  capture, structured responses, and comments. Prompt and reaction recurrence receive stricter runtime
  floors; launcher and inline retain their configured policy.
- [x] Preserve the current automatic embed and add declarative `<fidibeki-widget>` plus imperative
  `Fidibeki.mount()` placement through the same runtime. Keep `Fidibeki.track()` deferred to #7.
  Explicit mounts resolve stable keys, honor targeting/lifecycle/recurrence, return safe disabled
  results, reuse analytics, replay, submission, attachment, and error paths, and avoid a duplicate
  automatic mount.
- [x] Deliver all four presentations end to end: target and render, resolve locale, respect durable
  recurrence, submit one independent Feedback record with optional consented contact and existing
  attachments, and expose it in the inbox without creating an Issue. The runtime now resolves exact,
  primary-language, and project-default locale fallbacks; accepts rating, free-text, reaction, and
  single-choice responses; keeps legacy rating/text synchronized; and uses origin plus normalized path
  state with an in-memory storage fallback. The backoffice configures blocks, options, required state,
  consented contact, and locale variants, and the feedback detail exposes the submitted response.

Phase 1 exit gate passed on 2026-07-23 with no production work: migration `up/down/up`, all Go
packages, backend static analysis, web type/unit/full E2E, 97 widget unit tests, widget build, and all
25 widget E2E journeys passed. Staging deployed the expected commit across API, worker, web, and
widget; schema migration `55` is clean; health and post-deploy smoke checks pass. A disposable real
submission persisted the definition version and structured responses with no Issue relationship, then
was removed from the staging test tenant.

#### Phase 2: Operations Completion

- [x] Re-audit inbox, triage, Issues, notifications, outbox, RBAC, and reporter communication as
  complete/partial/missing; preserve implemented filtering, bulk triage, metadata, linking, merge,
  status history, and current delivery paths.
  - Complete and preserved: inbox filtering/pagination, bulk triage, priority, assignment, tags,
    independent Issue creation from one or many Feedback records, link/unlink, merge/unmerge, RBAC
    permission middleware, transactional outbox fan-out, delivery attempts, and signed generic
    webhooks.
  - Partial and extended in place: `issue_updates` records status transitions and optional transition
    comments only; reporter email covers opted-in Issue status changes but not receipt, clarification,
    portal access, or a complete public lifecycle; Slack and Teams have basic provider-native payloads
    but omit actor/change/attachment/resource context; generic webhooks are idempotent and signed but
    lack an explicit schema version and complete change envelope.
  - Missing: standalone comments, mentions and internal notifications, a complete immutable activity
    stream, `needs_info`, tenant/project reporter-access policy, opaque hashed/revocable progress
    tokens, the one-response clarification flow, and reporter receipt/final-resolution history.
  - Implementation path: first introduce one immutable activity/comment foundation and use it for
    every Issue mutation; add mentions on that contract; then build reporter access/communication and
    finally enrich all integration consumers from the same versioned events.
- [x] Add standalone internal comments, member mentions with internal notifications, and immutable
  activity coverage for every operational mutation and reporter communication.
  - Migration `56` adds the append-only Issue activity stream, independent comments, mentions, and
    member notifications. Issue creation, status/metadata/assignment/tags, links, merges, reporter
    responses, access changes, and delivered reporter messages now share that activity contract.
- [x] Complete reporter communication with receipt, important progress, `needs_info`, and final
  resolution messages; add secure hashed, scoped, revocable, rate-limited reporter access with
  tenant/project opt-out enforced at request time and a one-response clarification flow.
  - Migration `57` adds `needs_info`, tenant/project/Issue access policy, hashed resource-scoped
    tokens, and single-response clarification requests. `feedback.created` sends an idempotent receipt;
    Issue changes send progress/final messages with tenant-hosted links. Links expose only sanitized
    progress, remain valid across later emails, and are invalidated immediately by scoped revocation.
  - Backend tests cover receipt unsubscribe/complaint, policy inheritance compatibility, token
    revocation, older-link continuity, clarification response idempotency, and migration replay.
    Web type/unit tests and the complete 97-pass E2E suite cover operator, policy, and public flows.
- [x] Enrich Slack Block Kit, Teams, and additive versioned generic webhooks with complete context,
  changes, actors, comments, attachments, timestamps, and environment-aware resource links.
  Interactive Slack/Teams commands remain outside these three phases.
  - Provider-native messages now include operational facts and relevant Open issue, Open feedback,
    View page, and View attachments actions. Generic webhooks retain their existing envelope fields
    and add schema `1.0`, event/resource URLs, changes, actor, comment, classification, project, and
    attachment metadata. Outbox event keys associate Issue creation and updates with the immutable
    activity actor without adding another delivery path.
- [x] #27: add per-member, per-workspace notification preferences and windowed digest delivery.
  - Migration `115` adds `member_notification_preferences` (assignments, mentions, comments,
    important Issue changes, and new-feedback summaries, plus digest hour and weekday) and the
    `member_digest_deliveries` ledger. Defaults are low-noise, and the new-feedback category has no
    immediate mode in either the API contract or the database CHECK, so one email per feedback
    submission is unreachable by configuration.
  - All three cadences run through one windowed pipeline in `internal/membernotify`: immediate is a
    short window rather than a second notification path, so the transactional comment, mention, and
    assignment writes are untouched. Windows are half-open and claimed under a unique
    `(tenant, member, cadence, window_start)` key; empty windows are never claimed, only delivered or
    suppressed windows advance the cursor, unfinished windows are resumed at their own start, and
    each hand-off carries a delivery-scoped idempotency key.
  - Mandatory authentication, security, account-deletion, and billing mail keeps its own paths and is
    never consulted against a preference. Reporter consent and reporter unsubscribe remain on the
    feedback and Issue rows. Signed one-click opt-out confirms on `POST`, is scanner-safe on `GET`,
    and answers identically for valid, forged, expired, and unknown tokens so it cannot enumerate
    members or workspaces. Bounced and complained addresses are suppressed for this class and stay
    visible in the member's delivery diagnostics; preference and opt-out changes are audited in
    `tenant_access_events`.
  - Coverage: Go unit tests for defaults, mode validation, mandatory-class protection, timezone and
    weekday window math, retry determinism, and unsubscribe enumeration safety; container-backed
    tests for preference persistence and audit, exactly-once delivery, empty-window skipping, failed
    and abandoned window retakes, bounce suppression, and RBAC scoping; web unit and desktop/mobile
    E2E coverage of the settings page.

#### Phase 3: Widget Platform

- [x] Re-audit the shared runtime and configuration hierarchy before adding breadth.
  - Preserve: exact/prefix/regex include/exclude targeting and its URL tester, safe RE2 validation,
    durable per-page recurrence state, query/hash-independent canonical paths, trailing-slash
    normalization, all four presentation modes, localization/default locale, preview, and the
    existing definition status/objective contracts.
  - Partial: lifecycle exists in storage/runtime but public configuration currently removes inactive
    definitions before an explicit mount can distinguish `inactive` from `not_found`; the
    backoffice reduces lifecycle to one Active checkbox and mixes archived definitions into the
    primary list. Appearance has position/theme/accent and localized copy, but no controlled
    surface/text/logo hierarchy. Objectives are labels only, and diagnostics stop at last-connected.
  - Implementation path: close the lifecycle contract first; then add controlled inherited
    appearance; then deterministic objective policies; finally expose faithful preview, decision
    reasons, and installation diagnostics without replacing the targeting/runtime foundations.
- [x] Add controlled colors, logo, localized copy, language defaults, position, and presentation
  configuration without arbitrary CSS, DOM injection, or a general form builder.
  - Migration `58` adds typed JSON appearance layers at workspace, project, and definition scope,
    backfills legacy definition tokens, and retains the legacy public position/theme/accent
    projection. The resolved public contract adds surface/text colors and an HTTPS-only logo;
    validation enforces paired surface/text tokens, 4.5:1 contrast, and safe URLs. Backoffice forms,
    preview, and the isolated runtime share the same cascade and support explicit inheritance.
- [x] Add deterministic objective policies for continuous feedback, after engagement, content
  validation, and friction detection using only non-personal public/browser signals. Apply stricter
  confidence and cooldown rules to more interruptive presentations.
  - The automatic runtime now combines legacy trigger compatibility with a pure local scoring
    policy. It observes only active time, scroll depth, event counts, anonymous per-page/session
    visits, public metadata, and explicit boolean `data-fidibeki-*` signals. Launcher and inline stay
    immediately available; reaction and prompt use escalating objective-specific thresholds, and
    hidden-page time does not count. Explicit mounts remain deterministic and immediate.
- [x] Add `draft`, `active`, `paused`, and `archived` lifecycle behavior while preserving historical
  Feedback, analytics, and configuration; paused/archived widgets must neither render nor record
  impressions.
  - Public `rules` remains the active-only compatibility projection while `definitions` now includes
    inactive definitions so explicit mounts return `inactive` rather than `not_found`. Connection
    telemetry rejects inactive definitions. The editor exposes all four states, starts new rules as
    drafts, and hides archived definitions by default without deleting their history.
- [x] Complete backoffice preview, URL testing, decision explanation, and installation diagnostics.
  - The backoffice and runtime now consume one pure objective evaluator. A compact signal simulator
    reports score, threshold, contributing non-personal signals, trigger behavior, recurrence, and
    presentation guardrails without collecting new telemetry. The existing safe URL tester feeds a
    shared installation band alongside loader observation, configured domains, and active-definition
    state. Launcher closed/open and inline/reaction layouts now preview their actual presentation.
    Diagnostic refreshes preserve unsaved rule edits. Unit, type, coverage, build, and full responsive
    browser suites cover the shared contract and the desktop/mobile configuration journeys.

#### Post-roadmap issue sweep

- [ ] After the three pillars are complete, audit every open GitHub issue against code, tests, and
  staging behavior. Close issues that are already materially complete, then implement the remaining
  issues that do not carry the `postpone` label. Recommended planning decisions are pre-approved;
  the staging-only and no-production boundary still applies.
  - [x] Confirm Phase 0 issues #8-#12 are closed and retain passing regression coverage.
  - [x] Complete Feedback UI and widget contact issues #15, #17, #18, and #20.
    - Sentiment now uses one threshold-tested semantic badge with exact-score disclosure; status
      pills retain their complete labels in selected and ordinary rows across desktop and tablet.
      Individual, bulk-create, and link flows share a viewport-contained modal contract with an
      internally scrolling body. Optional reporter email is represented in configuration and live
      preview, validates inline without losing prepared feedback, and has explicit coverage for
      disabled, empty, email-only, and email-plus-consent payloads.
  - [x] Complete Settings quality issues #13 and #16.
    - Member and API-key forms now share a compact, responsive permission editor with explicit
      presets, custom-selection state, grouped native checkboxes, and inherited permission limits.
      Invitation email/domain failures remain adjacent to the email field with focus and ARIA error
      state, while transport failures stay at form level. Typecheck, unit, build, and responsive E2E
      coverage pass; one unrelated magic-link history timeout passed twice when rerun in isolation.
  - [x] Complete screenshot annotation issue #14.
    - Screenshot capture now opens a lazy, responsive annotation editor before attachment. Reporters
      can draw with controlled colors and brush size, undo or clear marks, confirm the annotated PNG,
      reopen it for editing, or remove it. Capture always stops its media stream, excludes the widget
      surface from the frame, preserves the existing upload contract, and cleans preview resources.
      Browser coverage verifies annotated output pixels plus desktop and mobile presentation.
  - [x] Complete moderated-content propagation issue #6.
    - Reporter content remains intact at rest for evidence and audit, while tenant feedback reads
      replace flagged/spam descriptions, text, copied comments/responses, and flagged attachment
      transcripts with `[MODERATED]`; a safe AI title remains available when present.
    - New Issues redact copied unsafe content before persistence, while independent operator-written
      summaries remain editable. Existing linked Issues and their creation activity are projected
      safely when their title or description contains moderated feedback content.
    - Integration delivery waits for the existing AI pass before any external request and then
      applies the same projection to Slack, Teams, and signed generic webhooks. Backend integration,
      unit, and responsive browser coverage verifies no raw moderated content crosses these paths.
  - [x] Complete shared transactional email issue #21.
    - [x] Add the shared minimal renderer and migrate every existing email family. Structured
      content now produces escaped, responsive HTML and equivalent plain text with an
      environment-aware logo, preheader, workspace context, metadata, one CTA, security-link
      fallback, purpose footer, Reply-To support, and no tracking or arbitrary HTML.
    - [x] Add the one-time founder welcome and remaining high-value lifecycle messages, then render
      and inspect representative desktop/mobile previews before staging validation.
      - Migration `59` preserves old workspaces while scheduling one independent, retryable welcome
        after the first owner activation for new workspaces. The personal sender and Reply-To are
        configurable and validated; the message contains no authentication token.
      - Material persisted Stripe transitions and account-deletion cancellations emit transactional
        outbox events. Billing managers and the deduplicated billing contact receive payment failure,
        recovery, activation, plan-change, cancellation, and deletion-cancellation notices with
        stable provider idempotency and actor context where available.
      - The shared wordmark now keeps its source aspect ratio and a clean blocked-image fallback.
        Welcome and payment-failure previews were rendered and inspected at 1280 and 390 pixels;
        all Go packages, migration replay, static analysis, web unit/type checks, and the Nuxt build
        pass. `docs/transactional-email.md` records inventory, sender, consent, and retry policy.
  - [x] Complete ownership, privacy, and abuse controls in #24, #25, and #26.
    - [x] #24 slice 1: add the explicit ownership invariant, append-only tenant access audit,
      current authorization-version enforcement, atomic recent-auth ownership transfer, and the
      owner-visible responsive Settings workflow.
      - Audit result: ownership is currently inferred from member order; permission changes do not
        revoke sessions or current access tokens; no member lifecycle/removal routes or organization
        access ledger exist. Reuse the tenant transaction, refresh-session revocation, permission
        model, and existing Settings member editor rather than adding another auth service.
      - Approved design:
        `docs/superpowers/specs/2026-07-23-workspace-ownership-and-recovery-design.md`.
    - [x] #24 slice 2: add protected deactivate/reactivate/remove operations, last-administrator
      checks, immediate session invalidation, denied-operation audit, and responsive member actions.
    - [x] #24 slice 3: make platform provisioning owner-backed, add recent-auth support recovery
      with verification evidence and audit, document the recovery runbook, and validate concurrency,
      isolation, and the complete staging journey.
      - Completion: every workspace now has a persisted owner; transfer, member lifecycle, and support
        recovery are transactional, recently authenticated, session-revoking, and append-only audited.
        Legacy and current tenant JWTs are checked against live membership state and authorization
        versions on every request. PostgreSQL migration replay/isolation, backend coverage, static
        analysis, frontend checks, and desktop/mobile ownership journeys pass locally.
    - [x] #25: audit and implement data access/export/deletion/retention controls after #24.
      - Audit result: tenant deletion, plan-based replay cleanup, and bounded analytics retention
        already exist and must remain the authoritative paths. Completed attachments have no retention
        cleanup, replay deletion loses its manifest, `notify_reporter` does not preserve consent
        history, and there is no export or verified reporter-rights workflow.
      - Approved design:
        `docs/superpowers/specs/2026-07-23-data-rights-export-retention-design.md`.
      - [x] Slice 1: durable workspace export jobs, allowlisted JSON/JSONL ZIP generation, private
        object upload, short-lived signed download, active-human authorization, audit, and retry.
        PostgreSQL migration/RLS, reconciled deterministic queueing, tenant-atomic claim, consistent
        archive snapshot, failure recovery, HTTP authorization, signed-download audit, full backend
        tests, and `go vet` pass locally.
      - [x] Slice 2: verified reporter export/anonymization, independent consent history, media and
        progress-token cleanup, public status, and exact-identifier isolation.
        Hashed, expiring verification tokens and neutral rate-limited public endpoints now create
        one reconciled reporter job. Reporter exports use an allowlisted point-in-time scope;
        anonymization removes stored media before transactionally clearing contact/DOM/transcript
        data while preserving operational history and retry safety. Consent timestamps, RLS,
        cross-tenant isolation, audit, terminal failure/retry, migration replay, full backend tests,
        and `go vet` pass locally.
      - [x] Slice 3: attachment/replay manifest-preserving retention, export/request expiry,
        bounded storage reconciliation, effective-retention API, and technical privacy inventory.
        Plan boundaries now delete attachment and replay objects at 30/90/365 days while retaining
        non-downloadable manifests, clearing derived transcripts, and retrying partial storage
        failures. A bounded, timed maintainer expires seven-day archives, 48-hour unverified
        requests, and 30-day verified access with audit records. `tenant:read` exposes the effective
        retention matrix, `docs/privacy-data-map.md` documents the technical/legal boundary, migration
        replay and retention boundary tests pass, and the backend coverage gate is restored above 80%.
      - [x] Slice 4: responsive Data & privacy Settings controls, reporter status page, desktop/mobile
        E2E, CI, and staging validation.
        Local implementation now exposes the plan-effective retention matrix, workspace export
        generation/history/download/retry, verified reporter export/anonymization requests with
        destructive confirmation, manual-review disclosure, and a neutral token-scoped public status
        and download page. Signed download schemes are constrained to HTTP(S), token pages ship
        no-store/no-referrer/noindex response headers, and desktop/mobile journeys pass.
        Commit `4fd55a5` passed the full backend gate and official CI before staging promotion.
        Authenticated Tauview validation queued and completed a workspace export in one attempt,
        issued and consumed a short-lived signed download, verified the 38 KB ZIP inventory, and
        rendered Data & privacy responsively at 1280 and 390 pixels without horizontal overflow.
    - [x] #26: audit and implement layered public-submission abuse protection after #25.
      - Audit result: public-key/origin authorization, honeypot, a 1 MiB request bound, basic
        project/network rate limits, plan quota, per-kind attachment declarations, one-time
        completion tokens, replay decoding, moderation, and a global provider limiter already exist.
        Attachment count/aggregate bytes, privacy-preserving coordinated scopes, upload expiry,
        payload signatures, quarantine before preview/AI, replay cost limits, per-project AI budget,
        stable widget retry UX, and tenant-safe diagnostics remain incomplete.
      - Approved design:
        `docs/superpowers/specs/2026-07-23-public-submission-abuse-protection-design.md`.
      - [x] Slice 1: shared HMAC buckets and named policies, stable error details, aggregate
        diagnostics/API, declaration bounds, and distributed/concurrency coverage.
        Local implementation now derives rotating HMAC identities before Redis, composes named
        project/origin/network/visitor policies, preserves the existing error envelope while adding
        stable reason/retry fields, and supports weighted byte budgets. A forced-RLS hourly ledger
        records only tenant/project/category/reason counts, exposes a bounded `tenant:read`
        diagnostics API, and is cleaned after 90 days. Feedback validates four unique attachment
        slots and a 125 MiB aggregate before storage, records honeypot/quota/rate pressure, retains
        text when an unavailable limiter denies optional upload grants, and keeps legacy handler
        compatibility. Focused backend, PostgreSQL/Redis concurrency, migration replay, and static
        analysis pass locally; the shared CI and staging gate completed in slice 4.
      - [x] Slice 2: explicit upload expiry, quarantine and deterministic signature verification,
        safe-only preview/AI/export, cleanup/reconciliation, and failure coverage.
        Migration `65` preserves completed legacy objects as safe and expires abandoned
        declarations while introducing an independent attachment safety lifecycle. Completion now
        consumes an unexpired upload into quarantine; a reconciled deterministic worker validates
        exact size and supported file signatures before promotion, persists rejection before object
        cleanup, and never marks operational scanner failures safe. Streaming, tenant reads,
        integration delivery, privacy exports, and attachment AI accept only safe non-retained
        objects. Migration replay, backend integration/unit/static checks, frontend type/unit/build,
        full responsive E2E, focused desktop/mobile screenshots, and local gitleaks pass.
      - [x] Slice 3: coordinated replay protection, verified batch lifecycle and aggregate bounds,
        idempotent tenant/project AI budget, defer behavior, and concurrency coverage.
        Replay grants now share the privacy-preserving project/origin/network/visitor policies,
        enforce session and weighted-byte limits before storage, expire deterministically, quarantine
        every uploaded batch, and accept only bounded structurally valid rrweb arrays before
        finalization. AI text generations and safe-attachment attempts reserve durable tenant/project
        daily units exactly once across retries, defer intact feedback until the next UTC budget
        window, expose that state to the inbox, support explicit recovery, and record only aggregate
        budget/provider diagnostics. PostgreSQL migration replay, Redis/PostgreSQL concurrency,
        backend integration, widget unit, and static contract checks pass locally.
      - [x] Slice 4: widget retry/error experience, responsive Submission health diagnostics,
        desktop/mobile E2E, CI, and staging validation.
        The widget portion is complete locally: stable public reasons map to localized inline copy,
        JSON/`Retry-After` delays drive an accessible countdown, and retry preserves the original
        idempotency ID, responses, reporter contact, and local attachment declarations. Accepted
        feedback remains successful when optional upload completion fails. Settings now exposes the
        existing tenant-scoped diagnostics as a compact six-category operational view with bounded
        hourly trend, top reasons, period/project filters, and explicit empty, retry, and permission
        states. Focused unit and responsive Chromium coverage passes with inspected desktop/mobile
        output and no horizontal overflow. The full backend gate now passes at 80.0% internal
        coverage with every package above 60%; this also exposed and fixed PostgreSQL timestamp
        inference in stale attachment reconciliation. Web and widget type/unit/build/browser suites,
        responsive screenshots, `gofmt`, `go vet`, golangci-lint, govulncheck, gitleaks, Compose,
        and all three local container builds pass. Independent correctness/performance review also
        drove forward-only migrations `68`/`69`, server-owned immutable media promotion, real
        container parsing, replay finalization sealing, retryable object cleanup outside API
        transactions, bounded disk staging, queue isolation, and indexed AI maintenance. The final
        local regression gate and official CI pass. Staging Submission health rendered real
        aggregate diagnostics responsively without overflow. A fresh Tauview session submitted
        four-star feedback with `201`, created no Issue, persisted `submitted` under the normalized
        `/en` page identity, remained suppressed for query/hash and `/en/` variants, appeared in the
        inbox with completed analysis, and was archived after validation.
  - [x] Complete workspace identity and organization profile issues #30 and #31.
    - Audit result: tenant name, widget appearance inheritance, object storage, member permissions,
      and append-only access audit already exist. Organization metadata, bounded image processing,
      versioned identity assets, self profile management, and shared logo/avatar rendering are
      missing. Extend those contracts rather than adding a separate identity or public upload
      service.
    - Approved design:
      `docs/superpowers/specs/2026-07-23-fidibeki-organization-identity-design.md`.
    - [x] Slice 1: forward-compatible tenant/member profile schema, cleanup queue, validation,
      normalized organization profile updates, audit, and preserved appearance inheritance.
    - [x] Slice 2: bounded PNG/JPEG/WebP processing, immutable 64/256 PNG variants, workspace logo
      and self-avatar lifecycle, RBAC, rate limiting, versioned reads, and retryable cleanup.
    - [x] Slice 3: Settings/profile editing and shared navigation, member, assignee, comment,
      mention, notification, and activity identity rendering with resilient initials fallbacks.
    - [x] Slice 4: migration/backend/web/browser/CI and real staging validation.
      Migration replay, full backend tests/vet, web unit/type/build/browser suites, widget
      unit/type/build/browser suites, desktop/mobile identity screenshots, field/read-only states,
      upload/remove flows, and broken-asset fallbacks pass. Commit `92496cd` passed backend, web,
      security, and release CI; Railway staging applied migration 71 and serves the matching
      API/worker/web source. The authenticated Tauview journey proved avatar and workspace-logo
      upload, immutable 64/256 reads through the tenant proxy, immediate propagation, removal,
      fallbacks, and cleanup without lock. Temporary identity data was restored.
  - [x] Complete member-aware internationalization issue #32.
    - Audit result: organization locale/timezone defaults and independent widget localization
      exist; member preferences, Nuxt i18n, SSR-aware precedence, regional formatters, localized
      API errors, message catalogs, and extraction enforcement are missing.
    - Approved design:
      `docs/superpowers/specs/2026-07-23-fidibeki-member-aware-localization-design.md`.
    - [x] Slice 1: additive member locale/timezone schema, self-service profile updates, session
      propagation, normalization, audit, and clear-to-inherit behavior.
      Migration 72 preserves existing members and enforces nullable regional preferences. The
      profile API now supports true partial updates, explicit inheritance clears, normalized
      supported locales, IANA timezone validation, one audit per real change, and preference
      propagation through member/session reads. Migration and tenant-member integration coverage,
      the full package test, and `go vet` pass locally.
    - [x] Slice 2: no-prefix Nuxt i18n runtime, SSR-safe precedence, Portuguese regional fallback,
      reactive document language/direction, and centralized regional/error formatting.
      Nuxt now resolves lazy no-prefix English, Portuguese, regional Portuguese, and pseudo-locale
      catalogs before SSR rendering, applies member/organization/browser/default precedence
      independently for locale and timezone, and reacts without hydration drift. Shared cached
      regional formatters cover temporal, numeric, relative, list, and plural output; localized API
      errors expose only allowlisted stable codes/reasons with safe generic fallbacks. Unit,
      typecheck, production-build, full-browser, and focused responsive SSR/runtime gates pass.
    - [x] Slice 3: profile/shared shell, authentication, and onboarding catalogs with immediate
      preference switching.
      English and Portuguese catalogs now cover shared navigation/account state, profile
      preferences, authentication, verification, access guidance, first-project activation,
      Turnstile, and targeting controls. Member locale/timezone updates apply without logout;
      public errors remain safe; regional dates, numbers, plan limits, titles, validation, and
      accessible names update consistently. Responsive English/Portuguese journeys plus unit,
      typecheck, build, and the complete browser suite pass.
    - [x] Slice 4: dashboard/analytics, feedback, issues, projects/widget configuration,
      settings/billing, and shared production catalogs without translating domain content.
      English and Portuguese catalogs now cover these surfaces while customer-authored content,
      exact API mutations, and regional formatting remain intact. Focused desktop/mobile workflows,
      unit tests, typecheck, and the Nuxt build pass.
    - [x] Slice 5: extraction audit, catalog parity, pseudo-locale responsive checks, complete local
      gates, CI, and real staging validation.
      The local gate is complete: source coverage, EN/PT parity, interpolation preservation,
      generated `en-XA`, unit (104), typecheck, build, and 18 integrated responsive browser journeys
      pass. Staging web deployment `c21ea9ce-a1b5-49dc-a779-7ca6213b4479` served the exact runtime:
      a real Tauview member inherited organization `pt`/`Europe/Lisbon`, switched to member
      `en`/`UTC` and `pt-PT`/`Europe/Lisbon`, then returned to null/null inheritance; public SSR
      resolved Portuguese through `Accept-Language`. Temporary authentication data was removed.
  - [x] Complete focused follow-up issues #38, #44, #45, #47, and #48.
    - Reactive widget-rule localization data is unwrapped before cloning and covered with a real
      Vue Proxy regression; desktop/mobile editor coverage verifies persisted configuration.
    - Owner badges and notification empty states use explicit centered geometry. Unrated feedback
      uses one compact localized metadata badge across dashboard, inbox, inspector, and detail.
      Destructive privacy actions retain a computed WCAG-AA hover contrast.
    - Unit (104), typecheck, build, focused responsive E2E, overflow assertions, and inspected
      screenshots pass locally. Staging refresh verification remains deferred with the staging gate.
  - [x] Complete reactive page-language-first widget localization from issue #40.
    - A shared root-language observer updates automatic and explicit/inline mounts without remounting,
      refetching configuration, losing draft/validation/presentation state, or duplicating funnel
      events. Exact and primary locale matching precede the project default; browser preferences are
      used only when the page language is absent or invalid.
    - Feedback metadata stores the resolved active locale. Legacy customer copy remains compatible
      while Fidibeki system copy follows supported page languages.
    - Widget unit (124), typecheck, build, and all 38 browser journeys pass locally.
  - [x] Complete replay correctness issues #33 and #41.
    - Fullscreen state now follows the browser Fullscreen API, preserves the recorded aspect ratio,
      keeps controls available, restores inline geometry, and survives repeated enter/exit cycles.
    - Canvas recording is an explicit per-rule opt-in with a validated 1-5 FPS bound, defaults off,
      and retains the existing `data-fidibeki-block` privacy exclusion plus all replay resource
      limits. Localized configuration explains the pixel risk and exclusion contract.
    - Migration replay, full backend tests/vet, web unit/type/build and responsive browser coverage,
      widget unit/type/build, all 39 widget browser journeys, continuously changing canvas
      performance/volume checks, and backoffice pixel reconstruction pass locally.
  - [x] Complete shared transient notification and destructive confirmation issues #34 and #35.
    - Workspace appearance success uses one global accessible toast without resizing the owning
      form or stacking repeated saves.
    - Project, rule, API-key, and integration deletion share one localized native-dialog flow with
      explicit consequences, initial Cancel focus, backdrop/Escape dismissal, focus restoration,
      pending guards, and local errors. No production `window.confirm` remains.
    - Web unit (104), typecheck, build, and focused browser journeys pass locally, including project
      deletion on desktop and mobile.
  - [x] Complete appearance and structured validation issues #36, #37, and #39.
    - Accent colors remain the only persisted input. Backend, backoffice, and widget deterministically
      derive the first WCAG-AA foreground from Fidibeki ink, white, then black; `#46c754` is accepted
      and renders with `#171a19`.
    - Reporter Access and Widget Defaults errors remain scoped to their forms. Rule validation
      preserves nested and unmapped fields, maps backend array paths, switches localization tabs,
      focuses the first actionable control, and clears only the edited path.
    - Full backend tests, web unit (113), widget unit (129), typechecks/builds, focused widget
      Chromium, and three focused backoffice browser journeys pass locally. No migration or
      deployment was required.
  - [x] Repair privacy request journeys from issues #46, #49, and #50.
    - Runtime-mode audit payloads are encoded for PostgreSQL JSON parameters, and public reporter
      privacy handlers are mounted on both platform and tenant hosts without weakening the neutral
      token response contract.
    - Generated-email URL, expiry, idempotency, tenant isolation, export, and anonymization
      integration coverage passes. Staging API deployment
      `06c035b6-9d10-47a5-8027-2420b648e843` applied migration 73 and passed first verification,
      re-verification, status, public page, and invalid-token checks; all synthetic data and the
      generated export object were removed afterward.
  - [x] Complete reporter progress issues #42 and #43.
    - The public page now follows the restrained Dispatch system with approved branding, localized
      loading/invalid/error/empty/clarification/answered states, and a responsive progress timeline.
    - Complete clarification responses render in the issue activity timeline with reporter
      attribution, chronological order, preserved newlines, long-text containment, and escaped
      interpolation. Backend contract coverage and 14 focused desktop/mobile browser journeys pass,
      including public submit to backoffice visibility and hostile HTML.
    - Staging web deployment `c21ea9ce-a1b5-49dc-a779-7ca6213b4479` passed a real reporter API and
      SSR journey in English and Portuguese; its temporary reporter token was removed.
  - [ ] Complete the new non-postponed privacy and governance issues before continuous improvement.
    - [x] #53: make replay anonymous by default across identifiers, locations, rrweb content,
      canvas, storage, streaming, playback, and deletion.
    - [x] #54: reuse the resulting privacy primitives for automatic feedback context, referrers,
      element attachments, backend validation, downstream projections, and existing-data
      remediation.
    - [x] #55: require explicit, structured, auditable, revocable reporter-update consent before
      email transmission, persistence, enqueue, or provider delivery.
    - [x] #56: make binary attachment declaration, upload, completion, retry, and degraded success
      reliable across supported browsers.
    - [x] #57: gate anonymous session replay behind independent managed/CMP consent with grant,
      reject, withdrawal, expiry, version-change, and zero-capture-before-grant behavior.
    - [x] #59: keep reporter anonymization pending until manual-review findings are explicitly
      resolved, preserving evidence and retryability.
    - [x] #58: complete purpose-specific widget analytics consent. Closed on 2026-07-24 after
      the founder decided D1: targeting visit state and funnel-analytics browser storage remain
      non-essential, consent-required storage (current shipped behavior); no functional
      exemption is claimed, and reclassification requires a new documented legal review.
      - [x] Technical core: independent managed/CMP evidence ledger, dormant default, zero
        persistent analytics identity/outbox/network before grant, withdrawal cleanup, backend
        fail-closed ingestion, and server-authorized optional `submitted` conversion.
      - [x] Local backend/web/widget gates and 54 browser journeys pass. Staging commit `f2e8211`
        is healthy on API `c6cf9de4-853a-41bc-8b10-221a44326a1d`, worker
        `a6676969-fcef-4069-8b3b-d853d932b5b3`, web
        `94426592-c796-49f5-b63a-1d12ff830996`, and widget
        `e1601da2-dad4-4c5f-aabf-1095c09d8e21`; migration `86` is clean.
      - [x] Disposable staging validation proved `422` before grant, `201` grant, `202` after
        grant, feedback remains usable with stale evidence, only valid evidence produces
        `submitted`, no Issue is auto-created, and all synthetic data was removed.
      - [x] Add tenant diagnostics for current analytics consent/configuration: a tenant-scoped,
        `widget_rule:read`-gated endpoint and rule-editor panel report per-rule consent
        mode/notice/active/expiring/denied/withdrawn/expired/recent counts and last-activity
        timestamp, scoped to the rule's current notice cohort, without exposing tokens, consent
        references, locale, or source.
      - [x] Closed the documented legal decision on targeting/functional storage: decision D1
        (2026-07-24) keeps the consent-required classification, recorded in
        `docs/widget-analytics-consent.md`.
    - [x] #60: complete data-rights case management and execution. Closed on 2026-07-24 after
      the authenticated staging gate passed end to end on `fb3f508` (case `PRV-60DFB5A0`:
      member intake, progress and decision communications with confirmed delivery, completed
      member export with valid ZIP download, terminal decision, append-only timeline,
      dashboard count, and the authenticated Data & privacy UI without errors).
      - [x] Slice 1: canonical reporter/member cases, verification, deadlines, ownership,
        idempotent lifecycle/events, explicit processing restrictions, fail-closed AI,
        integrations, reporter email, replay, and analytics enforcement, public status, and
        backoffice case intake/list/dashboard.
      - [x] Restriction details now populate case lists as well as detail views.
      - [x] Slice 2a: verified reporter access cases queue the existing scoped export and expose an
        authenticated, audited download; erasure cases queue the existing structured
        deletion/anonymization primitive without relabelling that primitive as the legal decision.
        Reasoned legal holds are tenant-isolated, idempotent, visible in the backoffice, and block
        erasure both at scheduling and immediately before worker side effects. Failed jobs remain
        retryable after a hold is reviewed and released.
      - [x] Slice 2b1: reporter discovery now includes linked consent, delivery, widget-submission,
        replay, AI, and privacy-request evidence. Portability assessments are append-only,
        idempotent, tenant-isolated, reasoned, and approve an explicit data-category set and
        `jsonl_zip` format before execution; partial approvals filter unapproved files from the
        generated archive. Bilingual desktop/mobile E2E covers assessment and execution gating.
      - [x] Slice 2b2: member access, rectification, erasure exceptions, and decision evidence.
        - [x] Member access cases queue a case-linked `member_export` job and produce a
          repeatable-read, tenant-scoped allowlist archive with the member profile, session and
          magic-link metadata without hashes/tokens, subject-authored issue activity,
          subject-addressed mentions/notifications, sanitized access events, and case history.
          Other members' content, raw audit payloads, credentials, queue data, avatar binary, and
          internal legal-review notes are explicitly omitted. Scheduling, completion, terminal
          failure, retry, authenticated download, isolation, and the omission contract are audited
          and covered through migrations `89` and `90`, PostgreSQL integration tests, and
          bilingual desktop/mobile backoffice E2E.
        - [x] Verified member rectification cases apply an explicit subset of `name`, `locale`, and
          `timezone` to the canonical member profile. Migration `91` preserves append-only
          before/after evidence, reason, actor, idempotency, tenant isolation, case activity,
          subject-aware access audit, and member-export history. The bilingual backoffice keeps the
          case open while applying corrections, refreshes the visible member profile without
          remounting the workflow, and exposes the immutable correction history in desktop/mobile
          E2E.
        - [x] Verified reporter rectification cases expose only subject-owned feedback within the
          request cutoff and atomically correct canonical `description` plus legacy `text`.
          Migration `93` preserves append-only before/after evidence, reason, actor, idempotency,
          tenant isolation, safe access audit, and case history. Text-derived AI output is cleared
          behind a generation fence and re-enters only the existing governed pipeline; stale
          reporter exports expire, while the feedback title and linked Issue remain independent.
          The bilingual backoffice provides selection, correction, and desktop/mobile history.
        - [x] Erasure case execution now reuses the append-only review-item evidence to expose
          itemized redacted, deleted, non-personal, retained-exception, and pending counts without
          returning raw content. Terminal decisions fail closed: completion requires settled
          execution with no exception or active hold, partial fulfilment requires a recorded
          exception or hold, and refusal is only available before execution. Partial/refused
          erasure outcomes require a decision summary, legal-review notes, and exception basis.
          Transition idempotency covers the complete evidence payload. The bilingual backoffice
          renders the item ledger and reasoned decision workflow, with PostgreSQL integration and
          desktop/mobile E2E coverage.
      - [x] Slice 3: recipient action tracking, correction/erasure notifications, communications,
        failure recovery, and the complete bilingual case-detail workflow.
        - [x] First operational vertical: migration `95` adds tenant-scoped recipient actions and
          per-attempt delivery evidence for verified rectification and settled erasure cases.
          Creation is blocked until the corresponding correction or erasure evidence exists,
          idempotently writes the action, case activity, safe access audit, and outbox event in the
          request transaction, and never treats enqueueing as delivery. The email worker records a
          provider-confirmed `sent` result, stores a bounded generic failure instead of provider
          detail, prevents concurrent automatic/manual retry sends with a short processing lease,
          and uses one provider idempotency key per action. The message contains only workspace,
          public case reference, action kind, and the operator-approved instruction — never raw
          feedback, subject/reporter email, or legal-review notes. The bilingual case workflow
          exposes evidence-gated creation, pending/processing/sent/failed state, attempt count,
          failure recovery, and retry. PostgreSQL integration, worker/resolver, frontend
          typecheck, and desktop/mobile E2E cover the vertical.
        - [x] Migration `97` adds explicit case-administration communications for verified
          reporter/member subjects. Progress, clarification, and decision messages are gated by
          case state, idempotent, tenant-isolated, queued through the transactional outbox, and
          preserve provider-confirmed delivery attempts, bounded generic failures, explicit retry,
          a short processing lease, and a stable provider idempotency key. These operational
          rights-request messages do not reuse non-essential reporter-update consent; safe case
          events/audit contain identifiers and kind only, never email, subject, or body.
        - [x] The bilingual case detail now loads its authoritative resource before exposing
          actions and presents subject/verification/ownership evidence, scope, append-only
          timeline, communication history/retry, and the dashboard's failed-external count.
          Migration/RLS, backend/worker, typecheck, and desktop/mobile E2E pass locally.
        - [x] Deployed the integrated revision to staging and passed the authenticated staging
          E2E on 2026-07-24, closing Slice 3 and #60.
    - [x] #61: add explicit, auditable AI governance, provider/purpose/input controls, sensitive
      data quarantine, policy disablement, and external provider-assessment gates.
    - [ ] #62: enforce lifecycle retention and legal holds for workflow, authentication,
      delivery, audit, and telemetry data.
      - Audit result: existing retention only covers attachments/replay objects (#25), export
        archives and reporter request expiry (#25), and widget analytics (#58), all via bounded
        `MaintainOnce`/`Run(ctx, interval)` maintainers wired as worker goroutines. Every other
        category in #62's scope — `tenant_member_sessions`/`platform_admin_sessions` (no
        `expires_at` column at all; expiry is implicit app-side), `tenant_magic_links`/
        `platform_admin_magic_links` (authoritative `expires_at`, but never swept after it
        passes), `outbox_events`, `delivery_logs`, and `member_notifications` had no cleaner and
        no maintainer wired in `main.go`. Legal holds (`privacy_case_legal_holds`) are scoped to
        a specific erasure case's subject data, not general infrastructure retention, so they do
        not gate this slice's cleanup.
      - Tracer bullet: `tenant_magic_links` (login and invitation credentials) was the smallest
        vertical whose expiry is already authoritative — `ConsumeMagicLink` already rejects
        `expires_at <= now()`, so a swept row never had remaining function and deleting it removes
        stored credential material rather than inventing a new legal retention period. Migration
        `98` adds `idx_tenant_magic_links_expires_at`. A new `tenantmember.Maintainer` +
        `tenantmember.MaintenanceStore` (mirroring `privacy.RetentionMaintainer`/
        `widgetanalytics.Maintainer`'s batched-drain shape) hard-deletes expired rows —
        consumed or not — in `LIMIT`/`FOR UPDATE SKIP LOCKED` batches and records a same-statement
        `auth.magic_link_expired` audit event in `tenant_access_events` (link ID and whether it
        was consumed only; no email, token, or permission content). Wired as an hourly worker
        goroutine. `GET /v1/privacy/retention` now reports an `auth_magic_links` policy category;
        `docs/privacy-data-map.md` and the bilingual Data & privacy settings copy were updated to
        match. Boundary (exact-`expires_at` sweep), idempotent-rerun, cross-tenant-isolation,
        audit-content, batch-limit, and concurrent-sweep (no double-delete/lost-row) coverage
        passed against real PostgreSQL; migration replay picked up migration 98 automatically.
      - [x] Executable registry slice: `GET /v1/privacy/retention` now derives its backward-compatible
        response from a versioned, typed, code-owned registry with 27 lifecycle categories and
        explicit `enforced`, `configurable`, `decision_required`, and `external_control` states.
        Migration-DDL tests prove that all 67 current PostgreSQL tables are assigned exactly once;
        object-prefix, queue, browser-key, and provider families are separately inventoried.
        Validation rejects duplicate ownership and unsupported enforcement claims. The bilingual
        Data & privacy surface exposes state counts, plan/rule duration, cadence, hold behavior,
        role, and resource coverage without presenting `Workspace lifetime` as an effective
        maximum. This descriptive slice adds no migration, cleaner, expiry clock, or deletion
        authority.
      - [x] Platform-admin magic-link tracer: `platform_admin_magic_links` had the same authoritative
        `expires_at` shape as `tenant_magic_links` but no audit-event sink, since platform-admin
        actions have no tenant scope to write `tenant_access_events` into. Migration `102` adds
        `idx_platform_admin_magic_links_expires_at` and a new minimized, append-only
        `platform_security_events` ledger (server-issued id, bounded dotted `kind`, bounded
        `resource_type`, opaque `resource_id`, JSON `data`, unique on the three identity columns),
        granted only `SELECT, INSERT` to `fidibeki_admin` and nothing to `fidibeki_app`. A new
        `platformadmin.MaintenanceStore.ExpiredMagicLinks` mirrors `tenantmember`'s shape: one
        statement selects/locks/deletes expired rows (`FOR UPDATE SKIP LOCKED`) and inserts one
        `auth.platform_magic_link_expired` event per deleted row recording only whether it was
        consumed — no email, token hash, admin id, IP, or user agent, and an event-insertion failure
        rolls back the deletion. `platformadmin.Maintainer` wires the same batched-drain (200/batch,
        20 batches, one-minute phase timeout, hourly) as an admin-pool worker goroutine beside
        `tenantmember.Maintainer`. `platform_admin_magic_links` moved into the enforced
        `auth_magic_links` registry category; `platform_security_events` was added to
        `authentication_security`, which remains `decision_required`. Boundary/idempotent-rerun/
        consumed-unconsumed/minimization/batch-limit/concurrent-sweep/atomic-rollback coverage
        passed against real PostgreSQL; the registry now covers 68 tables across the same 27
        categories; migration replay picked up migration 102 automatically.
      - [x] Magic-link cleanup health vertical: migration `105` adds one constrained, current-state
        row for each of the tenant and platform-admin magic-link cleaners. The ledger stores only
        schedule metadata, last attempt/success, bounded aggregate counters, budget exhaustion, and
        the generic `cleanup_failed` code; it contains no tenant/person/credential identifiers, raw
        errors, or append-only run history. Both hourly maintainers must record every successful,
        failed, or budget-exhausted sweep through a shared `retentionhealth` recorder; recorder
        failure is surfaced without rolling back already-completed idempotent cleanup. Read-time
        derivation classifies each operation as `healthy`, `attention`, `failed`, `overdue`, or
        `not_observed`, with a 15-minute grace window. `GET /v1/privacy/retention` additively returns
        the health report and the bilingual Data & privacy surface shows summary, alert reason,
        last success, next expected run, and last/total processed counts on desktop and mobile.
        Migration/grant/down-up, state-boundary, atomic-counter, stale-update, maintainer error,
        HTTP authorization, typecheck, and responsive Playwright coverage pass locally. The
        executable registry now covers all 74 tables; no legal period, external monitoring
        provider, production deployment, or uninstrumented cleaner was added.
      - [x] Sentry error telemetry scrubbing: the backend registered no `BeforeSend` hook at all
        (only `SendDefaultPII: false`), so a recovered panic's raw `Error()`/`fmt.Sprintf` text
        reached `Exception.Value`/`Message` verbatim; the widget scrubbed by deleting known-bad
        keys from a regex over field names, which kept a query-stripped `request.url` and never
        touched a thrown error's own `message`/`stack` text. `backend/internal/observability/scrub.go`
        (`ScrubEvent`, wired as `InitSentry`'s `BeforeSend`) and the rewritten
        `widget/src/modules/error-reporting.ts` (`scrubEvent`) now both reconstruct the outgoing
        event from an explicit allowlist instead: exception type/structural stack frames/timestamp/
        release/environment (plus a known widget phase and bounded `project_id`/`widget_rule_id`)
        survive; request (URL/method/data/query/cookies/headers), user/IP, frame absolute paths/
        source context/locals, breadcrumbs, arbitrary contexts/tags, and the free-text exception/
        message value are always dropped or replaced with a fixed placeholder. A field neither
        function names — including one a future
        `sentry-go`/`@sentry/browser` upgrade might add — is absent by construction. The `#63`
        provider governance row's `PersonalDataCategories` text and `docs/privacy-data-map.md`'s
        Audit and observability row were tightened to describe the actual allowlist. Sentinel tests
        on both sides cover email/token/raw-URL-with-query-and-fragment/headers/cookies/body/
        user/IP/breadcrumb-data/arbitrary-nested-context-and-tag/thrown-string-and-`Error`-message
        removal while proving type/structural-stack/timestamp/release/environment evidence survives;
        invalid or oversized operational-context values are also rejected, and one
        backend test exercises the real `sentry.Init`-with-`BeforeSend` → `RecoverWithContext` →
        transport path, not just the pure function. See
        `docs/superpowers/specs/2026-07-24-sentry-telemetry-scrubbing-design.md`. This slice adds no
        migration and does not touch the provider's retention period, region, legal entity, or
        contract status, which remain `external_control`/`decision_required`.
      - Remaining cleanup-policy slices: `tenant_member_sessions`/`platform_admin_sessions` retention
        (blocked on a founder/legal decision for security-record retention, since these carry
        IP/user-agent and are arguably security evidence — no `expires_at` exists yet either, so
        this needs schema work, not just a sweep); `outbox_events`/`delivery_logs`/
        `member_notifications` retention; workflow/feedback/issue/comment/AI-derived-text
        retention; IP/user-agent minimization; Sentry/error telemetry retention (the provider's
        retention period, region, legal entity, and contract status remain unverified — scrubbing
        itself is now enforced, see above); consent-evidence-after-withdrawal retention;
        tenant-configurable shorter retention;
        general legal-hold enforcement for the categories added here; overdue-cleanup
        metrics/alerts for the remaining enforced cleaners. Published legal periods for
        security/audit/billing records still require founder/legal review before any of those
        categories can be swept.
    - [ ] #63: establish layered notices, role matrix, DPA/subprocessor/transfer inventory,
      ROPA, and DPIA governance without publishing unverified claims.
      - [x] Provider-governance foundation: a typed code-owned registry covers the 12 external,
        customer-configured, infrastructure, and internal runtime boundaries currently represented
        by the retention inventory, including Cloudflare Turnstile. Each row records evidenced
        purpose, personal-data categories, activation mechanism, boundary kind, and explicit
        `unknown`/`pending` legal entity, location, transfer, contract, owner, review, and approval
        fields. The derived controller/processor role matrix covers all 27 retention categories.
        Validation rejects drift between the two registries, placeholder approval bypasses,
        inconsistent or stale approval evidence, and unbounded/duplicate category lists;
        `make governance-check` is the focused local gate. This is deliberately not a static
        outbound-call scanner, does not classify every service as a subprocessor, and publishes no
        unverified legal claim.
      - [x] Contextual customer-controller notice and version evidence: migration `103` adds the
        workspace privacy contact, rights contact, and notice version plus localized per-definition
        purpose, lawful-basis summary, retention summary, and policy links. Public widget config v3
        exposes the complete controller profile and resolved summaries without tenant-internal
        fields; contact collection fails closed when the notice is incomplete. Feedback persists
        the server-authoritative notice/consent snapshot, propagates that evidence into linked
        Issues, and preserves legacy records. The widget renders the disclosure and links in the
        actual form; the bilingual backoffice manages the profile, previews the resolved notice,
        and warns on incomplete contact collection. Backend, web, widget, responsive E2E, migration,
        vet, formatting, and repository-wide regression checks pass locally.
      - [x] ROPA/DPIA screening foundation: a typed code-owned registry
        (`backend/internal/privacy/ropa_registry.go`) groups the retention registry's 27 categories
        into 19 Article-30 processing activities covering each category exactly once. Role and
        recipient/provider references are derived from, and validated against, the retention and
        provider governance registries rather than hand-duplicated. Each activity carries
        data-subject (including a closed `third_parties` class)/data-category classifications,
        security-control references, a lawful-basis status with a named, non-placeholder reviewer
        paired with its review timestamp, and a DPIA screening snapshot (EDPB WP248 risk indicators
        — including the correctly-named ninth "prevents rights or service use" criterion, not a
        transfer criterion — outcome, reason, and owner/review/approval evidence). Every activity
        starts, and today remains, `lawful_basis: pending` and `dpia.outcome: pending`;
        `PublicationReady` is computed from completeness rules rather than a literal field (a source
        literal that presets it fails validation), and a `dpia.outcome: required` screening can
        never satisfy that gate on its own — only a fully evidenced `not_required` outcome can,
        because this slice has no model for evidencing that a required DPIA was actually completed.
        Processor-role activities' lawful-basis reasoning is explicit that Article 28 is a
        processing-instruction/safeguards framework, not itself an Article 6 lawful basis.
        Validation rejects retention-category coverage drift (missing, duplicate, or unknown
        categories), role inconsistency between an activity and its member categories, unknown
        recipient providers, placeholder lawful-basis/DPIA evidence, stale reviews,
        approval-before-review timestamps, a preset `PublicationReady`, and an inconsistently paired
        reviewer/review-timestamp, reusing the provider governance registry's staleness window and
        placeholder detection. `make governance-check`'s pattern now covers `Governance`, `ROPA`, and
        `DPIA` tests. No lawful basis, DPIA decision, or legal approval is recorded.
      - [x] Provider code release gate: every direct Go dependency and dependency in the widget,
        web, and marketing manifests is explicitly classified as application, build/test, or a
        governed provider boundary. Non-test backend source is parsed to inventory
        standard-library outbound HTTP constructors, and every provider row must retain verified
        source signals. New or stale dependencies, HTTP call sites, provider references, and code
        evidence fail `make governance-check`; the existing Sentry row now also evidences the
        widget's `@sentry/browser` path. This detects the repository's supported adapter entry
        points without claiming that an environment enables a provider or that arbitrary
        lower-level transports can be statically excluded.
      - Remaining #63 slices: founder/legal review of actual provider entities, regions, contracts,
        transfer mechanisms, roles, owners, and review dates; approved/published provider register
        and Article 28 DPA; provider change authorization/objection workflow; founder/legal lawful-
        basis identification and DPIA screening decisions (required/not-required) for each of the 19
        processing activities, with mitigations, owners, and approval evidence; staging
        reconciliation, link tests, localized E2E, and a reviewed change-notification/objection
        process for newly approved providers.
    - [x] #64: add a restricted personal-data breach register, immutable chronology,
      processor-to-controller notifications, decision evidence, and staging exercises. Closed on
      2026-07-24 with founder approval after the staging tabletop covered all five scenarios
      (attachment disclosure, replay access, database cross-tenant exposure, leaked integration
      secret end-to-end with delivered initial/update/closure notifications, and subprocessor
      breach via provider intake) with synthetic data only. The drill exposed that action
      revisions failed under fidibeki_admin grants (SELECT ... FOR UPDATE on the append-only
      ledger); fixed with an integration test that runs the lifecycle under the production role.
      The revision re-verification runs after the fix deploys to staging; the register's own
      retention policy (D14) and DPA notification terms (D26) stay tracked under #62/#63.
      - [x] Slice 1 (tracer bullet): platform-admin-only breach incident records and append-only
        chronology. Migration `92` adds `breach_incidents`, `breach_incident_tenants`, and
        `breach_chronology_entries` with no `fidibeki_app` grant at all — the fidibeki_admin-only
        pool used by the existing platform-admin architecture is the enforcement boundary, so
        tenant members and tenant API keys cannot reach this data regardless of application
        wiring, and both tables reject `UPDATE`/`DELETE` outright. Creating an incident captures a
        server-issued public ID, server-authoritative `created_at`, explicit `awareness_at`,
        detection source, controller/processor role, bounded affected environment/system/data/
        subject categories, confidentiality/integrity/availability impact, a containment summary,
        and affected tenant references (validated to exist, exposed only as bare tenant IDs, never
        tenant name/slug/billing content). Chronology entries stamp `occurred_at`/`created_at` from
        the same server clock read, record actor identity, entry kind, and bounded evidence
        references. Both operations are idempotency-key/fingerprint protected like the existing
        privacy-case pattern. New `platform:breach:create/read/append` permissions gate three new
        `/admin/breach-incidents` routes wired through the existing `RequirePlatformAdmin`
        middleware. Backend coverage includes platform-admin-audience/permission
        authorization (including explicit denial of a tenant-audience token), validation, store
        idempotency/conflict, immutability-trigger, and a migration 92 up/down/up plus a
        `fidibeki_app`/`fidibeki_admin` privilege-boundary check. Staged notifications, the 72-hour
        authority deadline, Article 34 assessment, subprocessor intake, and templates remain for a
        later slice.
      - [x] Slice 2 (tracer bullet): server-authoritative 72-hour authority-notification deadline
        plus append-only, reasoned breach assessment evidence. Migration `96` adds
        `authority_notification_deadline_at` to `breach_incidents` (GDPR Article 33's clock,
        started at `awareness_at`, never at completion of the investigation) and a new
        `breach_assessments` table, both still outside the `fidibeki_app` grant boundary and both
        immutable via the same append-only trigger as migration 92. The deadline can't be a
        `GENERATED` column — `timestamptz + interval` is only `STABLE` in Postgres, not
        `IMMUTABLE` — so the store computes and inserts it itself from `awareness_at` at create
        time (the same server-authoritative pattern already used for `created_at`/`occurred_at`),
        while the migration temporarily removes and immediately recreates the append-only trigger
        for its controlled existing-row backfill; a `CHECK` constraint guards the deadline can
        never drift. Each assessment row is a full
        reasoned-decision snapshot rather than a partial patch: an explicit
        `authority_notification_decision`, `high_risk_to_individuals`, and
        `data_subject_communication_decision` (each `assessment_pending` /
        `*_required` / `*_not_required`, with pending supplied explicitly — nothing is inferred
        automatically from the incident's facts), each paired with mandatory, bounded reasoning
        text restated every time. Bounded, optional facts ride along on the same row: paired
        min/max affected-subject and affected-record estimates (both bounds required together, so
        a lower bound alone can't misrepresent an exact count), likely consequences, recovery
        measures, and bounded evidence references (the reference-validation logic is now shared
        with chronology entries via `normalizeEvidenceReferences` instead of duplicated). `GET
        /admin/breach-incidents/{id}` now returns `assessments` alongside `chronology`; a new
        `POST .../assessments` route reuses the existing `platform:breach:append` permission —
        recording an assessment is the same actor class and trust level as appending a chronology
        entry, so no new permission was added. Coverage adds unit tests for the assessment
        normalization bounds, HTTP authorization/validation tests for the new route (plus a
        `assessments` field visibility test on `GET`), and PostgreSQL integration coverage:
        assessment idempotency/conflict/not-found, the computed-deadline value, immutability on
        `breach_assessments`, the `fidibeki_app`/`fidibeki_admin` privilege boundary extended to
        the new table, and — new versus slice 1 — an explicit migration 96 up/down/up cycle that
        exercises the down migration directly (slice 1 never had one) plus a full-chain migration
        test update (`latestMigrationVersion` in `internal/db/migrations_test.go`) and a real
        pre-existing incident backfill that proves the immutable trigger is restored. At
        completion of that slice, later work still included staged processor-to-controller
        notifications (delivery evidence, retries, follow-up updates),
        notification/closure/post-incident templates,
        contact/DPA wiring to #63's role matrix, subprocessor incident intake, containment
        checklists, post-incident actions/owners/corrective-issue links, and the tabletop
        exercises across attachment disclosure, replay access, database cross-tenant exposure,
        leaked integration secret, and subprocessor breach.
      - [x] Slice 3 (containment and corrective-action ledger): migration `101` adds
        `breach_action_events`, an append-only full-revision ledger outside the `fidibeki_app`
        grant boundary. New incidents atomically receive nine code-owned pending controls covering
        API keys, sessions, integrations, object storage, database access, email, AI, analytics,
        and replay; existing incidents receive the same checklist with their original creator and
        creation time, so migration does not imply later response work. Explicit post-incident
        actions require an existing platform-admin owner and future due date. State revisions
        require `expected_revision`; completion requires an owner, bounded outcome, and evidence
        reference, while not-applicable requires an owner and reason. Terminal actions reopen only
        through `in_progress`. Database constraints and an insert guard preserve immutable
        action identity, monotonic revisions, state requirements, and append-only history;
        transaction locks plus idempotency fingerprints make retries deterministic and allow only
        one concurrent writer to advance a revision. Incident detail additively returns current
        `actions` and complete `action_events`; `POST .../actions` and
        `POST .../actions/{actionID}/revisions` reuse `platform:breach:append`. Migration
        up/down/up, legacy backfill, grants, constraints, HTTP authorization, domain validation,
        idempotency/conflict/concurrency, and repository-wide backend checks pass locally. The
        executable retention registry covers the new table as `decision_required`; no cleaner,
        deployment, notification, or operational containment action was added.
      - [x] Slice 4 (staged processor-to-controller notifications): migration `104` adds frozen,
        versioned contact/content snapshots, append-only provider-attempt evidence, and a dedicated
        `fidibeki_admin`-only outbox so even an opaque breach event never enters the tenant-visible
        runtime role. Platform admins explicitly stage, queue, and retry initial, update, and
        closure notices; staging resolves the affected workspace's governed privacy contact and
        fails closed for non-processor incidents, unrelated tenants, or incomplete profiles.
        The worker uses a five-minute lease and stable provider idempotency key, records only a
        bounded generic failure, and sends minimized incident facts without evidence bodies,
        credentials, reporter content, raw payloads, IP/user-agent data, query strings, or signed
        storage URLs. Incident detail additively exposes the frozen snapshots and delivery ledger.
        PostgreSQL lifecycle/grant/immutability tests, HTTP audience and permission tests, worker
        failure/retry coverage, migration `104` down/up, retention-registry coverage across 71
        tables, and repository-wide backend gates pass locally. No notification was sent and no
        deployment was performed.
      - [x] Slice 5 (restricted documentation exports): two read-only, platform-admin-only GET/HEAD
        routes under the existing incident resource --
        `.../controller-notifications/{notificationID}/export` and `.../post-incident-report` --
        download either JSON or Markdown. Both reuse `platform:breach:read` (downloading an
        already-recorded fact is the same trust level as reading it) and both call the existing
        `Store.GetIncident` -- the same call `getIncident` already makes -- so no migration, new
        query path, or N+1 access was added. The notification export serializes one frozen
        `ControllerNotification` (any kind/sequence) and its append-only delivery attempts
        unmodified; the post-incident report additively renders incident facts, full chronology,
        the single most recent assessment (an explicit JSON `null` when none has been recorded,
        never omitted or inferred), current action state plus a derived per-action revision-count/
        timestamp history summary, and the full staged-notification/delivery ledger. Neither
        document computes or asserts notifiability, high-risk status, closure, or compliance,
        and both end with the same non-conclusory footer already used in the notification email.
        Every response carries `schema_version`, `generated_at`, the incident public ID, and the
        existing stable resource IDs, plus safe `Content-Type`/`Content-Disposition`/an explicit
        `Content-Length` (the exact rendered byte length)/`Cache-Control: private,
        no-store`/`X-Content-Type-Options`/`Referrer-Policy` headers; `HEAD` mirrors `GET`'s status
        and headers, including `Content-Length`, with an empty body. Markdown rendering is at full
        field parity with JSON -- every stable ID, actor/owner/tenant ID, and lifecycle timestamp,
        with explicit `(pending)`/`(none)` markers for unset optional fields -- and is
        injection-safe by construction: short operator/customer-controlled values (workspace name,
        recipient email, title, provider message ID, corrective reference) are backslash-escaped
        and have embedded newlines/control characters turned into visible markers via
        `mdEscapeInline`, while long narrative free text (summaries, reasoning, outcomes,
        containment/measures text) is wrapped in a fenced code block via `mdSafeBlock`, whose fence
        is chosen one backtick longer than any backtick run already in the text so heading-like,
        table-like, or fence-breaking input can never restructure the document. The post-incident
        report's per-notification section reuses the exact same renderer as the standalone
        notification export, so the two documents stay in parity by construction. Coverage adds
        deterministic-rendering, pending-value, nil-slice-normalization, forbidden-field
        shape-guard, Markdown-escaping/safe-block, and adversarial heading/table/fence-injection
        unit tests plus HTTP tests for audience/permission denial, malformed/unknown/mismatched
        IDs, missing/invalid `format`, exact frozen notification content, matching
        `Content-Length` across `GET`/`HEAD`, and both formats for both documents.
      - [x] Slice 6 (external-provider incident intake): migration `106` adds an append-only,
        `fidibeki_admin`-only provider-notice ledger plus an incident-scoped tenant mapping. Each
        intake resolves one stable provider ID from the code-owned governance registry and freezes
        the registry version, display name, boundary kind, approval/contract status, and primary
        retention category known at receipt time; pending/unknown legal fields remain pending/
        unknown, and internal-runtime boundaries are rejected. The bounded record includes a
        provider notice reference, provider-reported timestamp, server-authoritative receipt time,
        factual summary, opaque evidence references, one or more governed data-flow categories
        including the provider's primary category, platform-admin actor, and one or more tenant IDs
        already affected by the parent incident. It stores no raw provider payload, credential,
        signed URL, IP/user-agent, reporter content, or evidence body, performs no external call,
        and does not label the provider a confirmed subprocessor. `POST
        /admin/breach-incidents/{incidentID}/provider-intakes` reuses
        `platform:breach:append`; incident detail and the JSON/Markdown post-incident report
        additively expose the same frozen facts from the existing repeatable-read snapshot.
        Idempotency fingerprints include both normalized operator input and the frozen governance
        snapshot, preventing a retry from silently re-resolving against a changed registry.
        Domain, HTTP, export-safety/parity, PostgreSQL scope/idempotency/grant/immutability,
        retention-registry, and migration `106` up/down/up coverage pass locally across all 74
        tables. No provider was contacted and no deployment or production work was performed.
      - [x] Slice 7 (read-only platform operations UI): the existing platform-admin verification
        flow now issues a second short-lived, HttpOnly, SameSite=Strict access cookie scoped only to
        `/admin/operations`; refresh and logout rotate or clear it together with the queue cookie.
        A server-rendered surface at `/admin/operations/privacy-incidents` reuses the existing
        `platform:breach:read` authorization and the established `Store.ListIncidents`/
        `Store.GetIncident` snapshot rather than adding a parallel API or data model. Its list and
        detail views expose factual incident state, assessment clock, chronology, current action
        revisions, staged controller notifications and delivery attempts, provider intakes, and
        exact JSON/Markdown export aliases. It is intentionally read-only: there is no browser
        mutation or JavaScript, so the surface cannot bypass the bearer API's idempotency,
        validation, or audit contracts and does not introduce an unaudited CSRF path. Restrictive
        no-store, framing, referrer, MIME-sniffing, and CSP headers apply to every HTML response;
        Go templates escape operator-controlled content. Desktop and 390px mobile screenshots were
        inspected, with document-width assertions proving only the internal data tables scroll.
        Authentication/audience/permission, escaping, empty/not-found, complete-section, exact
        bearer-export parity, HEAD, cookie lifecycle, and queue-navigation coverage pass locally.
        No incident was changed, no notification was sent, and no deployment or production work
        was performed.
      - Still remaining for later #64 slices: staged processor-to-controller notifications with
        an authenticated staging capture exercise; staging-only tabletop scenarios; and any future
        browser mutation forms, which must add explicit CSRF protection without weakening the
        bearer API contracts.
    - [x] #65: complete sensitive-access governance. Closed on 2026-07-24 after the
      post-migration-108 staging case `FID-65-STAGING-POSTFIX-20260724` proved denied/success
      support access, CSV/UI visibility, and immutable actor provenance after deleting the
      temporary platform admin.
      - [x] Core attachment, replay, and export access auditing is transactional and fail-closed,
        distinguishes human/API-key/system actors, provides tenant filters/CSV/anomaly counts,
        deduplicates range traffic, and has backend/web coverage.
      - [x] Bound CSV exports independently at 5,000 rows with an explicit UI limit, preserve the
        500-row JSON cap, exercise concurrent deduplication, and surface historical access by
        now-inactive members plus sensitive access by non-expiring API keys.
      - [x] Add a reusable platform-admin/support access primitive
        (`accessaudit.ValidateSupportRequest`, `PermissionPathSupportAccess`,
        `auth.PermissionPlatformTenantSupportAccess`, migration `94`) and one representative
        sensitive-resource path — `GET /admin/tenants/{tenantID}/feedbacks/{feedbackID}/attachments
        /{attachmentID}/support-access` — that requires platform-admin auth plus explicit,
        length-bounded support reason and case headers before any lookup, keeps justification out
        of URLs, preserves the same
        tenant/resource-scoped RLS authorization as the tenant-facing stream, records
        `actor_type=platform_admin` with the reason/case as safe metadata (never content, storage
        keys, or credentials), and audits denied/not_found/success outcomes fail-closed with the
        same dedup-window flood protection. Confirmed by construction and integration test that
        these events never carry a member identity, so they are excluded from the existing
        member-export sanitized access-events allowlist rather than needing a new export path.
      - [x] Extend the platform-admin/support access primitive to replay playback:
        `GET /admin/tenants/{tenantID}/sessions/{sessionID}/support-access` requires platform-admin
        auth plus the same explicit, length-bounded `X-Fidibeki-Support-Reason`/`X-Fidibeki-Support-Case`
        headers (query-parameter-only justification is rejected as invalid), reuses `Store.GetSession`
        and the tenant stream's ready/merged/storage-key contract under the same tenant-scoped RLS
        context, records `actor_type=platform_admin` with `replay_session.support_accessed` and the
        reason/case as safe metadata only, and shares the tenant route's merged-object streaming via
        an extracted `writeReplayStreamContent` helper. Both routes open the storage object before
        recording a successful access, so storage-open failure returns unavailable without leaving
        false success evidence; the helper then owns the already-open stream.
        Denied/not-found/success outcomes audit fail-closed with the same dedup-window flood
        protection as the attachment path. No migration was required (existing `accessaudit`
        primitives and the `replay_session` resource type from #65's core slice already cover this
        path). Integration coverage adds authorization (unauthenticated/denied-permission),
        missing-reason/missing-case/query-only-credential rejection with dedup, wrong-tenant and
        unknown-session not-found (each isolated to its own tenant), not-yet-merged session
        not-found, successful playback streaming the merged object with the correct headers, and
        audit-event identity/content-omission assertions.
      - [x] Extend the same platform-admin/support access primitive to the privacy-export access
        path: `GET /admin/tenants/{tenantID}/privacy-exports/{jobID}/support-access` requires
        platform-admin auth plus the same explicit, length-bounded
        `X-Fidibeki-Support-Reason`/`X-Fidibeki-Support-Case` headers, reuses `privacy.Store.GetJob`
        under the same tenant-scoped RLS context, and folds a not-yet-completed, expired, or
        otherwise unavailable export into the same not-found response as a missing job so support
        access never discloses more about a tenant's export lifecycle than "not currently
        downloadable". Unlike the tenant-facing download grant (which mints a 10-minute presigned
        URL the caller fetches directly from object storage), this path proxies the archive itself
        through `Storage.OpenObject` and streams it — a platform admin is never handed a signed URL
        or raw object key they could retain or forward outside the audited path. Records
        `actor_type=platform_admin` with kind `privacy_export.support_accessed` and the reason/case
        as safe metadata only; denied/not-found/success outcomes audit fail-closed with the same
        dedup-window flood protection as the attachment and replay paths. No migration was required.
        Integration coverage mirrors the replay path: authorization, missing-reason/case/query-only
        rejection with dedup, wrong-tenant and unknown-job not-found (isolated to their own tenant),
        pending and expired-export not-found, storage-open failure returning unavailable without a
        false success event, successful streaming with the correct `application/zip` headers, and
        audit-event identity/content-omission assertions (no object key, URL, or archive bytes).
      - [x] Add a tenant-visible indicator/filter for platform-admin/support access in the audit UI:
        each event row shows a "Support access" badge with the bounded case reference (full reason
        on hover) already held in the event's `data`, and a dedicated "Support access only" checkbox
        filters the ledger to `actor_type=platform_admin` — every such event is a support access by
        construction, so no new backend filter was needed. Added a `support_access_volume` anomaly
        signal (`accessaudit.Store.DetectSupportAccessVolume`, default 5 events/24h per platform
        admin — far lower than the general bulk-access threshold, since support access is expected
        to be rare and always case-justified) surfaced alongside the existing signals. Covered by
        backend unit/integration tests (store-level detection, HTTP-level signal wiring) and web
        typecheck/unit/e2e coverage (badge/filter rendering, signal display, i18n in en/pt).
      - [x] Tighten attachment support-access outcome evidence so an object-storage `OpenObject`
        failure cannot remain represented as a successful access after metadata lookup. Both the
        platform-support and tenant-facing attachment streams now open the storage object before
        recording a successful access (mirroring the replay fix), refactored `writeAttachmentStreamContent`
        to stream an already-opened reader instead of opening it after commit, and added integration
        coverage proving a storage-open failure returns 503 without a false `success` audit row on
        both paths.
      - [x] Ran the authenticated staging E2E for the completed support-access paths and tenant
        audit workflow on 2026-07-24; #65 is closed with that evidence.
    - [x] #66: add safe per-integration field profiles, disclosure ledgers, destination
      capabilities, restriction enforcement, and rectification/erasure action tracking.
      Closed on 2026-07-24 after the authenticated staging gate proved minimum disclosure to a
      temporary no-optional-fields destination, fail-closed restriction suppression, the manual
      rectification boundary with append-only correction evidence, the downstream checklist and
      Settings warnings in the authenticated UI, and safe deletion of the temporary integration
      while ledgers and case evidence remain.
      - [x] Add the first minimized-delivery slice with migration 99. New Slack, Teams, and generic
        webhook destinations default to a fixed minimum (public/resource identity, moderated title,
        project, status/priority, event time, and authenticated Fidibeki resource link); existing
        destinations are backfilled with the legacy optional groups so the migration does not
        silently break their payload contract. Per-destination allowlists can opt into description,
        origin/path-only source page, rating, classification, workflow context, and attachment
        metadata. Email addresses, query parameters, fragments, credentials, consent/replay
        identifiers, IP/user-agent data, and raw attachment evidence are never selectable.
      - [x] Apply the same projection after the existing moderation gate to Slack Block Kit, Teams,
        and generic webhook output. Generic webhooks keep schema `1.0` and gain additive `privacy`
        metadata. Workflow actor payloads use ID/display name only; source-page actions and payloads
        strip query and fragment; attachment actions remain authenticated Fidibeki routes rather
        than provider/object-storage URLs.
      - [x] Record a disclosure ledger on each delivery attempt without copying the payload:
        destination kind/ID, profile version, selected data-category names, delivery result, and a
        truthful capability snapshot. Incoming-webhook destinations explicitly report that
        delivered messages cannot be updated, deleted, or recalled while Fidibeki links remain
        authenticated and access-controlled. Restriction enforcement remains before the provider
        request and records a failed/suppressed attempt.
      - [x] Add owner UI for create/edit disclosure profiles with minimum-data summary, explicit
        opt-in checkboxes, external-retention warning, and synthetic preview. Desktop/mobile E2E
        covers creation and profile update; backend coverage proves allowlist validation, tenant
        permissions, minimized sentinels, URL stripping, ledger categories, capability truth, and
        existing moderation/retry behavior.
      - [x] Migration `100` adds a tenant-isolated downstream-action checklist derived only from
        successful disclosure-ledger rows in the verified privacy-case scope. It groups evidence by
        stable destination, preserves deleted integrations, unions category names without payload
        content, excludes failed/suppressed attempts, and reopens completed entries after a newer
        disclosure. Reporter scope follows the case email digest and receipt cutoff; member scope
        requires the matching subject-authored workflow activity.
      - [x] Current incoming-webhook destinations are truthfully marked as manual: completion is
        blocked until correction or settled-erasure evidence exists, requires idempotent operator
        evidence, and cannot bypass future API-owned rows. Recipient instructions can be associated
        with a checklist entry without treating email delivery as destination completion. Case
        activity and privacy audit records contain identifiers/category metadata only.
      - [x] The bilingual case detail exposes refresh, empty/manual/completed state, disclosure
        evidence, capability limits, completion evidence, and linked recipient notices. PostgreSQL
        lifecycle/isolation/API-boundary tests, recipient-worker regression, migration
        up/down/up/RLS/grants, frontend unit/typecheck/build, the complete local browser suite
        (212 passed; 54 configuration-gated skips), and focused desktop/mobile visual inspection
        pass locally.
      - [x] Ran the authenticated staging E2E for profile configuration, minimized delivery,
        restriction suppression, disclosure-ledger evidence, and the downstream checklist on
        2026-07-24; #66 is closed with that evidence.
    - [x] #67: refine language and timezone preferences layout.
    - [x] #68: fix sidebar navigation label alignment and overflow.
    - [x] #69: make the attention queue note actionable.
    - [x] #70: make replay and widget-analytics consent panels span the complete project-rule
      editor width. Long Portuguese copy now grows within the available row without colliding with
      the action; narrow viewports stack a full-width action below the copy. Focused desktop/mobile
      geometry assertions, overflow checks, and inspected screenshots cover both panels.
    - [x] #71: add an `edge_tab` launcher variant alongside the existing `floating_button`. Migration
      `107` adds a constrained, non-null `launcher_variant` column: existing rows backfill to
      `floating_button`, new/omitted rows default to `edge_tab`, and a check constraint rejects
      unknown values. The typed field flows through the domain model, input normalization/validation,
      the widget-rule store, and the public widget configuration. The widget renders one semantic
      launcher button, activation handler, panel, and analytics/lifecycle path for both variants;
      only the closed-state geometry differs — desktop `edge_tab` is flush to the configured side,
      vertically centered with a bounded height and vertical label; mobile becomes a compact
      horizontal edge-attached control so long localized labels wrap instead of clipping.
      `floating_button` keeps its inset geometry and now honours the configured side on mobile.
      The project editor adds an
      accessible two-option visual selector (native radios, Lucide icons, Edge tab marked
      Recommended) shown only for the launcher presentation, with a faithful closed-preview that
      reflects the selected variant and side; English and Portuguese copy is complete.
      - Backend: full `go test ./...`, `go vet`, and build pass; the PostgreSQL migration
        integration test proves existing-row backfill to `floating_button`, new-row default to
        `edge_tab`, invalid/null value rejection, and a full down/up cycle.
      - Widget: unit tests (missing-field default, class/attribute selection, side, accent, label,
        focus, one interaction event) and coverage thresholds pass; typecheck and esbuild build pass;
        15 new Playwright cases cover both variants, left/right placement on desktop and mobile,
        visible keyboard focus and activation, panel/submit/dismiss parity, long Portuguese
        wrapping without clipping, and no document overflow, alongside the full 69-test browser
        suite.
      - Web: unit, typecheck, and Nuxt build pass; two new Playwright cases cover the accessible
        selector and a faithful desktop preview (side/variant attributes, exact save payload); the
        full 219-test responsive suite (desktop + mobile) passes.
      - Desktop/mobile screenshots for both variants (including the long Portuguese wrap) and the
        editor's live preview were generated and visually inspected locally. Staging validation was
        not performed under the current no-deployment constraint.
    - [x] #72: replace the undiscoverable horizontally scrolling mobile Feedback filter toolbar
      with one complete, bounded filter disclosure while preserving the current dense desktop
      toolbar, URL synchronization, queue semantics, and backend contract. Approved design:
      `docs/superpowers/specs/2026-07-24-mobile-feedback-filters-design.md`; implementation plan:
      `docs/superpowers/plans/2026-07-24-mobile-feedback-filters-implementation.md`.
      At and below `700px`, `web/pages/feedback/index.vue` keeps the search field directly visible,
      replaces the horizontal-scrolling primary row with one native `Filters` disclosure exposing
      all ten structured filters (project, status, rating, evidence, moderation, Issue relationship,
      AI state, sentiment, from, to) bound to the existing reactive refs, and shows a pluralized
      active-count in the summary's accessible name once any of those ten is non-empty. Clear
      filters reuses the existing reset; Done closes the disclosure and restores focus to its
      summary, whose explicit focus ring remains visible after that restoration. The panel is a
      content-sized, viewport-bounded `position: fixed` overlay with internal scrolling only when
      needed (two columns, one column at 360px and below) and never creates document-level
      horizontal overflow. Desktop and tablet keep their exact existing toolbar, controls, and CSS
      untouched behind a `display: contents` wrapper. English/Portuguese strings were added for the
      trigger and Done; all existing filter labels and Clear filters copy are reused unchanged.
      Verification: web unit tests (133 passed), Nuxt typecheck, and Nuxt production build all pass.
      Three new focused Playwright cases were added to `web/tests/e2e/dispatch-platform.spec.ts`
      (desktop toolbar preservation, mobile disclosure discovery/filtering/clear/focus-restoration,
      and short/narrow-viewport geometry); three pre-existing tests that referenced the primary
      filter controls were updated to scope their locators to the correct desktop/mobile container
      now that both markup variants coexist in the DOM. The complete Playwright suite passed
      222/280 (58 intentionally skipped by viewport/project guards, 0 failed) on a clean run; two
      unrelated `activation-flow.spec.ts` magic-link tests flaked once under full-suite worker
      contention and passed both in isolation and on the following clean full run, matching a
      pre-existing timing-sensitive flake unrelated to this slice. Desktop, mobile (open/closed),
      short-viewport, narrow single-column, and Portuguese screenshots were generated and visually
      inspected locally, including a real layout defect (CSS Grid row-stretch creating large gaps
      in the mobile panel) found and fixed during that inspection. No deployment, staging, backend,
      or query-semantics changes were made.
    - [x] #73: make the dashboard attention queue use the curated Feedback title before the
      compatibility fallbacks to cleaned/raw reporter text. Fixtures must distinguish title from
      description so the independent field contract cannot regress unnoticed. `feedbackCopy()` now
      resolves `title -> text_cleaned -> text -> localized empty copy`; a responsive fixture with
      four distinct values proves the curated title and legacy cleaned-text fallback in desktop and
      mobile. All 133 web unit tests, Nuxt typecheck/build, and two focused Playwright journeys pass;
      inspected screenshots retain the existing row geometry and no document overflow. No
      deployment or staging validation was performed. Approved design:
      `docs/superpowers/specs/2026-07-24-dashboard-feedback-title-design.md`; implementation plan:
      `docs/superpowers/plans/2026-07-24-dashboard-feedback-title-implementation.md`.
    - The #53-#55 privacy foundation is integrated on `main` through `81de75c`. Backend tests and 80.1%
      coverage, widget unit/typecheck/build and 40 browser journeys, web
      unit/typecheck/build, migration up/down/up, and focused feedback/replay integration all pass.
    - Staging API `df9bda9a-f529-4971-ad88-f4f3512ebeb3`, worker
      `6778eeaf-36f5-4b83-b22b-50978d06d06c`, widget
      `de1f1549-fb70-42bb-be49-4ab108f6375e`, and web
      `764af152-e3be-4bcb-9d24-c48a362a4a0a` are healthy with migration 77 clean.
    - A disposable staging gate verified consent rejection without persistence, exact/prefix/origin
      URL projection in raw and daily funnel rows, unsafe element-context omission, anonymous replay
      privacy v2, no leaked sentinels, and complete cleanup without triggering email or text AI.
  - [ ] After the entries above are complete, repeat the full GitHub issue audit against code,
    tests, and staging, close materially completed issues, and implement every remaining issue
    without the `postpone` label before starting open-ended continuous improvement.
    - [x] Repeated the local code/test/GitHub audit after integrating #71 on 2026-07-24. No
      non-postponed issue has unimplemented local product scope: #58 and #63 await documented
      legal/factual decisions; #60, #62, #64, #65, #66, and #71 await deployment of the exact
      validated commits plus authenticated staging evidence. Each issue contains its current
      evidence and remains open rather than claiming an unperformed gate.
    - [x] Deployed the exact validated `main` revision `fb3f508` to staging and passed the
      authenticated gates for #60, #65, #66, and #71; those issues are closed with evidence,
      as are #72, #73, and #74. Production remains prohibited.
    - [ ] Run the remaining authenticated staging gates: #62 after its outstanding retention
      slices (sessions, delivery/outbox/notifications, workflow data, IP/UA, consent evidence,
      tenant-shorter policies, broad holds, and alerts) and #64 notification capture/tabletop,
      whose final acceptance also depends on #62/#63. #58 and #63 still require the outstanding
      legal/factual decisions and are not unlocked by deployment.
  - [x] Preserve postponed scope in #7, #19, #22, #23, #27, #28, and #29 without implementation.

Each phase ends with compatible migration `up/down/up` checks where applicable, focused backend,
web, and widget tests, responsive/accessibility verification, and one real staging journey. The
three-pillar roadmap does not authorize production rollout or infrastructure expansion.

### Existing capabilities to preserve and extend

- `feedbacks.issue_id` is already nullable and protected by tenant/project foreign keys. Existing
  links can remain intact while new feedback is stored without an issue.
- The feedback inbox already exposes linked/unlinked filters; it needs real create/link/unlink
  actions and broader bulk triage rather than a second filtering implementation.
- Issues already have public IDs, a status workflow, status history, soft merge, outbox events, and
  reporter notifications. Extend these contracts instead of replacing them.
- The Gemini text pipeline already makes one structured call for cleaned text, sentiment, and
  moderation. Add title, actionability, category, and priority recommendations to that response and
  persistence contract; do not add another paid call.
- Widget targeting already supports exact URL, prefix, regex, priorities, and safe invalid-regex
  failure. Extend it with include/exclude rules, save-time safety checks, and a URL tester.
- Widget suppression currently uses `sessionStorage` and is written when the trigger opens. Replace
  this with durable state written only by meaningful transitions such as submit or dismiss.
- `widget/connected` currently records project activity when a rule matches. Keep it for activation
  diagnostics, but do not count it as an impression.
- Transactional outbox fan-out, deterministic delivery tasks, Slack Block Kit, Teams MessageCard,
  signed generic webhooks, delivery logs, and issue update comments already exist. Enrich them
  additively instead of building another notification path.

### Vertical slice sequence

1. **Independent feedback and explicit issue creation.** Add feedback `title` and `description`, add
   issue `description`, preserve and backfill old data, stop auto-creating issues, keep the public
   submission response backward compatible, and let an operator create an editable issue from one
   feedback. AI recommendations remain suggestions only.
2. **Operational inbox triage.** Link and unlink one or many feedback records, then add priority,
   assignment, tags, and bulk triage with tenant/project/RBAC enforcement.
3. **Smart widget state and targeting.** Store per-project/rule/page states (`new`, `submitted`,
   `minimized`, `dismissed`, `snoozed`) in `localStorage`; support cooldown, recurrence, never-show,
   compact post-submit behavior, include/exclude targeting, and safe URL testing. A reopened widget
   starts a new submission with a new idempotency ID.
4. **Widget funnel and dashboard.** Record `matched`, `impression`, `interaction`, `submitted`, and
   `dismissed` with anonymous visitor, page-view, and event IDs. Provide deduplication, idempotency,
   batching/`sendBeacon` where appropriate, retention, daily aggregates, and useful project/rule/page
   and period breakdowns. Report interaction, completion, and submit rates from the correct
   denominators.
5. **Rich link-based notifications.** Enrich Slack Block Kit and Teams messages with event/public ID,
   title, description, project/page, rating/classification/priority, status transition, actor,
   comment, attachments, timestamp, and environment-aware tenant URLs. Add relevant Open issue,
   Open feedback, View page, and View attachments links. Add `schema_version`, event/resource URLs,
   changes, actor, and comment to the generic webhook without removing existing fields.
6. **Interactive integrations.** Add signed callbacks, Slack/Fidibeki identity mapping, RBAC, audit,
   idempotency, and explicit success/error handling before offering Assign to me, Create issue,
   Archive, Close, or Reopen. Incoming webhooks must never pretend to execute these actions.
7. **Collaboration and resolution communication.** Add comments, mentions, a complete activity log,
   and a reporter-facing resolution update across the issue lifecycle.
8. **Onboarding and initial configuration.** Make page targeting an explicit setup decision, explain
   existing matcher modes in context, and complete useful empty states and first-run configuration.

### Slice 1 acceptance criteria

- A new widget submission creates one feedback and no issue, regardless of rating.
- Existing feedback/issue relations and issue rows are unchanged by migration/backfill.
- `feedback.description` contains the original full widget text. `feedback.title` is an editable
  short summary with a deterministic first-sentence/truncation fallback.
- Issues own editable `title` and `description` fields.
- The existing AI text response may recommend actionability, category, priority, and title in the
  same call. Recommendations never create or link an issue automatically.
- The feedback detail presents Create issue only when unlinked. The confirmation form is prefilled
  with editable title and description and creates the issue plus link atomically.
- The public response no longer requires an issue ID. Existing clients can continue decoding the
  additive/optional response contract.
- Backend unit/integration, migration up/down/up, web unit/E2E, and widget contract tests cover both
  the new unlinked path and preserved legacy links before staging validation.

## Architectural decisions

- **Runtime & backend**: Go 1.25 (latest supported patch in CI/images), `chi` router, `slog` for
  structured logging. The module toolchain directive pins the local/CI patch release.
- **Database**: PostgreSQL 16, **Postgres Row-Level Security (RLS) as defense-in-depth**
  (not just app-layer `WHERE tenant_id`). Every tenant-scoped table has RLS enabled with
  `FORCE ROW LEVEL SECURITY` and policies `USING (tenant_id = current_setting('app.tenant_id')::uuid)`.
- **Two DB roles, two pools**:
  - `fidibeki_app` — RLS-enforced. Used by tenant-scoped HTTP handlers.
  - `fidibeki_admin` — `BYPASSRLS`. Used by platform routes, migrations, Stripe webhooks, and async workers.
- **Per-request transaction with transaction-local tenant context**: every request on the app pool
  runs inside a `pgx.Tx`; the tenant middleware calls
  `SELECT set_config('app.tenant_id', $1, true)` with a bound UUID at the start. It buffers the handler
  response, rolls back on panic, and only flushes the response after a successful commit.
  The transaction-local setting dies with the transaction — no GUC leak across requests, panic
  commit, SQL interpolation, or false-success response.
  Handlers take a `Querier` interface (satisfied by both `pgx.Tx` and `pgxpool.Pool`).
- **pgbouncer in transaction mode from day 1**: multiplexes many app connections onto few
  Postgres connections. `SET LOCAL` works in transaction mode; `SET` (session-level) does not.
  Prepared statements disabled (`default_query_exec_mode=exec`) to avoid pgbouncer version gotchas.
  Migrator uses `DATABASE_URL_DIRECT` (bypasses pgbouncer).
- **Migrations**: `golang-migrate` with SQL files in `infra/migrations`, one file per concept
  (table/enum/index-set), each paired with a `.down.sql`. CI runs an up→down→up test.
- **Multi-tenancy routing**: Subdomains of `fidibeki.com` resolve to a tenant via a
  `TenantResolver` interface. MVP ships `SubdomainResolver` only; `CustomDomainResolver` is
  a typed-out stub for the enterprise custom-domain feature. `BASE_DOMAIN` env var.
  `api.fidibeki.com` is the platform host (excluded from tenant resolution).
- **Host-based router split**: `api.fidibeki.com` serves the platform router and a dedicated public
  widget router. Platform routes use the admin pool. Public widget routes resolve the project/public
  key on the admin pool, then open an app-pool transaction and set transaction-local tenant context;
  all business reads and writes therefore remain RLS-enforced. `*.fidibeki.com` serves tenant routes on the app pool.
  Pool selection is router-group-level, never handler-selected. `X-Forwarded-Host` is NOT trusted
  (Caddy/forwarded header trust is a deploy-time decision).
- **Tenant lookup**: no cache for MVP — indexed `SELECT id, status, plan, trial_ends_at
  FROM tenants WHERE slug = $1` per request on the admin pool. Status is not cached to
  preserve the "suspended tenant can't transact" guarantee.
- **Middleware classification by chi route group**: four middlewares mounted per group —
  `Resolve` (always: subdomain → tenant, start tx, set transaction-local tenant context),
  `RequireActive` (write gate: 403 if suspended/expired-trial),
  `AllowDegraded` (read gate: suspended OK),
  `AllowAlways` (auth/billing gate).
  Plus `RequirePlanLimit(feature)` as a separate downstream middleware for 402s.
  Classification is by mounting, not by URL sniffing.
- **Authentication backoffice**: Magic-link login. Shared `internal/auth/magiclink` library
  built in Phase 1 (platform consumer); Phase 2 adds the tenant consumer.
  JWT access tokens (HMAC-S256, 10-min, `aud=platform|tenant`, `permissions` snapshot in claims)
  + opaque rotating refresh tokens (hashed in `*_sessions` tables, 30d, theft-detection via
  reuse-of-revoked-token). No passwords stored.
- **Token transport**: Access token in memory + `Authorization: Bearer` header; refresh token
  in httpOnly `SameSite=Strict` cookie. Same-origin (Nuxt + Go behind one Caddy reverse proxy
  on `*.fidibeki.com`) — no CORS on backoffice calls, cookies flow naturally.
  Silent refresh on app boot; silent refresh on 401 via `apiFetch` plugin.
- **Widget authentication**: Project public key (`pk_live_<22 base62>`) in `X-Fidibeki-Public-Key`
  header + strict `Origin` validation against `projects.allowed_domains` (exact match or
  explicit `*.domain` wildcard entries). Dynamic CORS origin echo, `credentials: 'omit'`,
  no `Access-Control-Allow-Credentials`. CORS is convenience, not the security boundary.
- **File storage**: S3-compatible storage in production. MinIO is pinned to the final official
  container release for local tests only because its server repository is archived. A single shared
  bucket uses `tenants/{tenant_id}/...` prefixed
  keys. The server exclusively constructs keys and presigns one exact key; production uses scoped
  STS credentials where the provider supports them. A shared root credential is not described as
  storage-layer tenant isolation. No versioning;
  write-once keys; lifecycle policy for retention (per plan's `storage_days`); async
  `delete_prefix` worker for explicit deletes. `Content-Disposition: attachment` at presign.
  Server validates MIME + size at `complete` via S3 HEAD; bucket policy denies dangerous MIME
  types (`text/html`, `application/javascript`).
- **Feedback submission wire format**: JSON metadata first → presigned S3 uploads for files →
  completion callback. `POST /v1/feedback` returns `{ feedback_id, presigned_urls[], complete_tokens[] }`;
  widget PUTs directly to S3; widget calls `POST /v1/attachments/{id}/complete` with a
  single-use completion token; server verifies token + S3 HEAD (size + MIME).
  `client_feedback_id` + `client_attachment_id` UUIDs provide idempotency
  (`UNIQUE(project_id, client_feedback_id)`, `UNIQUE(feedback_id, client_attachment_id)`).
  Presigned URL TTL: 10 minutes. `sweep_attachments` cron deletes orphaned uploads after 1h.
- **AI processing**: Google Gemini `gemini-2.5-flash` for all tasks (transcription, sentiment,
  moderation, OCR). Stream MinIO → Gemini File API → `generateContent` → delete Gemini file.
  Persist each remote filename before analysis; bounded inline deletion retries fall back to the
  durable `ai_gemini_files` cleanup ledger and reconciler.
  Per-attachment + per-text asynq jobs; `processing_status` state machine
  (`pending|processing|completed|failed|skipped`) with CAS transitions; revert to `pending` on
  transient failure, `failed` + `asynq.SkipRetry` on permanent (content policy, malformed media).
  `redis_rate` limiter keyed `gemini` with `GEMINI_RPM` env (default 15). Per-tenant daily AI
  budget enforced at enqueue time. Plan-gated: non-AI plans get `processing_status='skipped'`.
  Video uses Gemini's native video input (no FFmpeg in the Docker image).
- **Email delivery**: Resend via asynq `email-pool`. Two sender identities on one domain:
  `auth@fidibeki.com` (transactional — magic links, billing receipts) and
  `updates@fidibeki.com` (reporter-facing issue updates). Isolated reputation per sender.
  Go `html/template` + plaintext fallback, no MJML/React-Email for MVP. Async only.
  `delivery_logs` is one row per attempt (with `attempt_number`). Resend bounce/complaint
  webhook auto-unsubscribes reporters (`feedbacks.notify_reporter=false`).
  Unsubscribe via JWT (`aud=reporter-unsubscribe`, 90d TTL, dedicated middleware).
- **Queue**: `hibiken/asynq` on Redis. Domain changes write a transactional outbox row; an outbox
  dispatcher creates deterministic, idempotent tasks per destination. This replaces resource-level
  `delivery_dispatched` booleans, which cannot represent partial fan-out. Four isolated worker pools in one Go binary:
  `email-pool` (concurrency 20), `ai-pool` (concurrency 3), `notification-pool` (10),
  `maintenance-pool` (2). Per-task backoff policies. Terminal failures update the relevant
  DB row (`delivery_logs.status='failed'`, `attachments.processing_status='failed'`).
  Best-effort enqueue + 5-min `reconcile_pending_jobs` cron (self-healing if Redis blips).
  Jobs carry IDs only; workers re-fetch fresh state. A native read-only queue operations UI is
  mounted on `api.fidibeki.com` behind platform-admin auth.
- **Payments**: Stripe Checkout (hosted) + Customer + Subscription + one Price per plan.
  Price IDs in env vars (`STRIPE_PRICE_*`). Hybrid trial: Stripe-native trial +
  Fidibeki `trial_ends_at` enforcement + daily cron + Resend dunning (3d/1d/0d before end).
  6 webhook events with `stripe_events` idempotency table; handlers load-from-Stripe
  (event is a signal, Stripe is source of truth). Downgrades allowed: restrict-new-creation
  (not read-only-existing) + `pending_plan` applied at next period. Customer Portal for
  self-service card/cancel. Enterprise is platform-admin-managed, no Stripe integration.
  Plan limits return `402 Payment Required` with `upgrade_url`; rate limits return `429`.
- **Rate limiting**: `redis_rate` (GCRA) on Redis. Widget endpoints keyed per-IP + per-project;
  backoffice keyed per-member; platform keyed per-IP. Fail-open on Redis outage.
  Rate limit (429, per-IP/member, Redis) and plan limit (402, per-tenant, DB count) are
  separate middlewares; rate limit runs first. Per-project ceiling + honeypot field for MVP;
  Cloudflare Turnstile on signup from day 1; Turnstile on widget deferred to Phase 12.
  `X-RateLimit-Limit` / `X-RateLimit-Remaining` / `Retry-After` headers on every rate-limited response.
  API keys get a configurable higher limit (`api_keys.rate_limit_per_min`, default 1000).
- **Backoffice UI**: Nuxt 3 on `*.fidibeki.com`, hybrid SSR (`/login` + `/` SSR for auth
  bootstrap, rest SPA). One Nuxt app reads `Host` → `tenantSlug` in SSR context.
  Same-origin via Caddy reverse proxy (path-prefix routing: `/api/*`, `/v1/*`, `/auth/*`,
  `/billing/*` → Go; everything else → Nuxt). `apiFetch` plugin with silent-refresh-on-401.
  `useState` for auth (no Pinia for MVP). Server middleware for SSR routes, page middleware
  for SPA routes. `can(perm)` helper for authz only (plan limits separate, surface as 402 on click).
  No per-tenant branding for MVP (tenant name in header only).
- **Widget embed**: Vanilla JS, zero-dep. Single ~5KB `loader.js` + lazy-loaded ES modules
  (`rating.js`, `audio.js`, `video.js`, `screenshot.js`, `replay.js`, `error-reporting.js`) — only modules the plan allows
  are fetched. Shadow DOM for CSS/JS isolation. One-line embed:
  `<script src="https://widget.fidibeki.com/v1/loader.js" data-fidibeki-key="pk_live_..." async>`.
  Config fetched once, checked on SPA navigation (history patch + custom event), refreshed
  every 5 min and on `visibilitychange`. Three triggers (delay/scroll/exit_intent) with
  `sessionStorage` suppression. Attach-element captures HTML + computed styles; screenshot
  capture is a separate lazy module using the browser's display-capture permission flow.
- **Session replay**: rrweb (pinned version) with PII masking (`maskAllInputs`, `maskTextSelector`).
  Session-level sampling (`widget_rules.replay_sampling_rate`) + caps (30 min, 50k events).
  Continuous recording across SPA navigations, ends on `pagehide` or 5-min idle.
  Batch upload to S3 presigned PUT (one object per ~50-event batch, ~200KB each);
  post-session `replay.merge` job concatenates batches into `replay.json` and deletes batches.
  Player is `rrweb-player` in a Vue component; replay streamed through Go API (auth + RLS).
  `sessions.rrweb_version` + `sessions.merged_at` columns. Both `sessions.feedback_id` and
  `feedbacks.session_id` for bidirectional links.
- **Deploy target**: Railway (Docker + Postgres + Redis + optional S3/MinIO + pgbouncer).
  Multi-stage alpine Dockerfiles for Go API and Nuxt app (separate images). Widget is static
  files served by Caddy/CDN. GitHub Actions: PR pipeline (fast + integration, <8 min) →
  merge to main → auto-deploy staging → E2E post-deploy → manual promote to prod.
  Migrations run as part of deploy (`migrate up` before app start). Expand-then-contract
  for destructive changes. Real secrets in Railway only; CI uses mocks + testcontainers.
- **Testing discipline**: TDD with ≥80% total coverage + no package below 60% (excluding `main`/`cmd`).
  4-layer pyramid: unit (pure functions, mocked services), integration (testcontainers Postgres
  + Redis + MinIO, transaction-per-test, RLS positive+negative+admin tests per table),
  E2E (Playwright for widget + backoffice + Stripe test mode), contract (Stripe/Resend/Gemini fixtures).
  Widget: Vitest + `happy-dom` unit + Playwright E2E. Nuxt: Vitest for composables + Playwright E2E
  (skip component unit tests for MVP). Workers: test handler functions directly with mocked deps
  + 1 integration test per job type via real Redis testcontainer.
- **Observability**: `slog` JSON → stdout (Railway-collected) with fields: `time, level, msg,
  request_id, tenant_id, tenant_slug, route, duration_ms, user_id, error`. PII redaction custom handler
  (scrubs `text`, `email`, `transcription`, `dom_snapshot_html`, `storage_key`).
  Sentry SaaS for Go + widget + Nuxt with source maps and PII scrubbing.
  Skip Prometheus for MVP (derive from logs + the queue operations UI). `/healthz` (liveness, always 200) +
  `/readyz` (readiness, checks DB+Redis+S3 with 2s timeouts). Widget lazy-loads Sentry SDK
  on first error (preserves 5KB loader). `docs/runbook.md` with 10 scenarios.
- **API contract**: Flat single resources / `{ data, meta: { has_next, next_cursor } }` collections.
  Error envelope `{ error: { code, message, details? } }` with 13-code catalog
  (`bad_request`, `unauthenticated`, `forbidden`, `tenant_suspended`, `not_found`,
  `unknown_tenant`, `conflict`, `validation_error`, `rate_limit_exceeded`,
  `plan_limit_exceeded`, `plan_feature_unavailable`, `internal_error`, `service_unavailable`).
  `402` for plan limits (distinct from `429` rate limits). `500` includes `sentry_event_id`.
  `402` responses include `upgrade_url`. `Location` header on `201`. `X-Request-ID` on every
  response. URL path versioning (`/v1/`). JSON only (`application/json; charset=utf-8`).
  Widget maps `error.code` → friendly user-facing messages (`widget/src/messages.ts`).
- **Config**: Env vars + `.env` for local dev via `godotenv`. Typed `Config` struct with
  `Validate()` at startup (exit with clear error if invalid). ~35 env vars grouped by concern.
  Secrets vs. non-secrets convention; `Config.Redacted()` for safe logging.
  `plans.go` is code (not env) — plan features/limits are product decisions; Stripe Price IDs
  are env (deploy-specific). `.env.example` committed and maintained.
- **Permissions**: 32 tenant permissions (`resource:action`, no wildcards) + 9 `platform:*` permissions.
  3 UI presets (admin/editor/viewer) stored as explicit `permissions[]` arrays. `RequirePerm`
  middleware per route. First member (founder) gets all permissions. API keys use the same model.
  Accept up to 10-min JWT staleness on permission changes (token refresh picks up new perms).
- **Tenant deletion**: Self-service with 7-day grace period + platform admin force-delete.
  `tenants.deletion_requested_at` column; middleware treats non-null as read-only.
  Daily `tenant_deletion_executor` cron: cancel Stripe sub, revoke sessions, delete S3 prefix,
  `DELETE FROM tenants CASCADE`, insert into `tenant_deletions` audit table. Soft during grace,
  hard after. No automatic refunds (manual via Stripe dashboard). Data export deferred to Phase 12+.
- **Self-service signup**: `POST api.fidibeki.com/v1/signup` (no auth, Turnstile-protected) creates
  a `trial` tenant + first admin member. Lazy Stripe customer creation (on first billing interaction).
  Trial gets the chosen plan's limits. Magic-link verify flow: the email opens Nuxt
  `GET /auth/verify#token=...`; client code reads and immediately clears the fragment, then sends the
  token to Go `POST /auth/verify`. Fragments are not sent to the web server or request logs. Delete
  unverified tenants after 24h. First-come-first-served slugs.

## Data model principles

All tenant data is stored in a single PostgreSQL database and isolated by `tenant_id` with RLS.
For the MVP we avoid JSONB entirely: every attribute that matters is a typed column.
Arrays and enums are allowed where the set of values is bounded and queryable.
Tables with a direct `tenant_id` use the following fail-closed policy. Root and indirectly-owned
tables use explicit equivalent policies documented with their migration; `tenants` compares `id`
to the tenant setting and grants `UPDATE(name, billing_email)` only, while tables such as
`widget_rules` and member sessions isolate through their owning project/member.
```sql
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <t> FORCE ROW LEVEL SECURITY;
CREATE POLICY <t>_tenant_isolation ON <t>
  FOR ALL TO fidibeki_app
  USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON <t> TO fidibeki_app;
```

## Enums

```sql
CREATE TYPE tenant_plan AS ENUM ('starter', 'growth', 'scale', 'enterprise');
CREATE TYPE tenant_status AS ENUM ('active', 'suspended', 'trial', 'cancelled');
CREATE TYPE matcher_type AS ENUM ('all_pages', 'url_exact', 'url_prefix', 'url_regex');
CREATE TYPE trigger_type AS ENUM ('delay', 'scroll', 'exit_intent');
CREATE TYPE attachment_kind AS ENUM ('screenshot', 'audio', 'video', 'replay');
CREATE TYPE processing_status AS ENUM ('pending', 'processing', 'completed', 'failed', 'skipped');
CREATE TYPE feedback_status AS ENUM ('open', 'spam', 'archived');
CREATE TYPE issue_status AS ENUM ('open', 'in_progress', 'resolved', 'closed');
CREATE TYPE integration_kind AS ENUM ('slack', 'teams', 'webhook');
CREATE TYPE delivery_status AS ENUM ('pending', 'sent', 'failed', 'bounced');
CREATE TYPE event_type AS ENUM ('feedback.created', 'issue.created', 'issue.updated', 'issue.resolved');
CREATE TYPE widget_position AS ENUM ('bottom-left', 'bottom-right', 'top-left', 'top-right');
CREATE TYPE replay_mode AS ENUM ('none', 'sampling', 'all');
CREATE TYPE widget_theme AS ENUM ('light', 'dark', 'auto');
```

Note: `member_kind` was removed — API keys live in a separate `api_keys` table.
Note: `email` was removed from `integration_kind` — reporter emails are not integrations.

## Key tables

- `tenants` — root company accounts with typed plan, status, billing, trial, deletion columns.
- `tenant_members` — human users belonging to a tenant. Access via explicit `permissions` text array.
- `api_keys` — machine credentials with `token_hash`, `permissions`, `rate_limit_per_min`.
- `tenant_member_sessions` — hashed rotating refresh tokens, revocable.
- `tenant_magic_links` — single-use magic-link tokens (SHA-256 hashed, 10-min TTL).
- `platform_admins` + `platform_admin_sessions` + `platform_admin_magic_links` — platform-level auth.
- `projects` — a site or app monitored by a tenant. Embed key and allowed domains are typed columns.
- `widget_rules` — per-page widget configuration with typed matcher/trigger/form/replay/theme columns.
- `feedbacks` — a captured user response with typed rating, text, AI columns, metadata, DOM snapshot, status.
- `attachments` — media linked to a feedback with typed transcription, sentiment, moderation, processing state.
- `sessions` — session replay bundles with rrweb version, merge state, event counts.
- `issues` — grouping of feedbacks with typed public id, status workflow, soft-merge support.
- `issue_updates` — status change history for an issue.
- `issue_counters` — per-(tenant, year) atomic sequence for public id allocation.
- `integrations` — external service connections (Slack, Teams, webhook) with typed config + webhook secret.
- `delivery_logs` — per-attempt audit trail of every outgoing notification with status and error.
- `stripe_events` — webhook idempotency table (event_id PK).
- `tenant_deletions` — compliance audit log of deleted tenants (not tenant-scoped, no RLS).

## Detailed database schema

### `tenants`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| name | TEXT NOT NULL | Display name of the company |
| slug | TEXT NOT NULL UNIQUE | Subdomain identifier, 3-24 chars, `[a-z0-9-]`, not reserved |
| plan | tenant_plan NOT NULL DEFAULT 'starter' | Billing tier |
| pending_plan | tenant_plan NULL | Scheduled downgrade, applied at next period |
| status | tenant_status NOT NULL DEFAULT 'trial' | Account lifecycle state |
| billing_email | TEXT | Invoice/billing contact |
| email_bounced | BOOLEAN NOT NULL DEFAULT false | Set by bounce webhook on billing_email |
| stripe_customer_id | TEXT | Stripe Customer object id (lazy-created) |
| stripe_subscription_id | TEXT | Stripe Subscription object id |
| trial_ends_at | TIMESTAMPTZ | Null when not on trial |
| deletion_requested_at | TIMESTAMPTZ | Non-null = in 7-day deletion grace (read-only) |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Constraints**: `CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{1,22}[a-z0-9])?$' AND slug NOT IN
('api','www','admin','mail','widget','auth','billing','app','staging','dev','test','demo','support','help'))`

**Indexes**: `idx_tenants_slug`, `idx_tenants_status`, `idx_tenants_stripe_subscription_id`

**RLS**: self-read + self-update (name, billing_email only) via app role; all other writes via admin role.

### `tenant_members`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| email | TEXT NOT NULL | Login/contact email |
| name | TEXT | Display name |
| permissions | TEXT[] NOT NULL DEFAULT '{}' | Explicit permission strings (31 possible) |
| email_bounced | BOOLEAN NOT NULL DEFAULT false | Bounce webhook on this email |
| email_complained | BOOLEAN NOT NULL DEFAULT false | Complaint webhook on this email |
| last_login_at | TIMESTAMPTZ | Updated on magic-link verify |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Constraints**: `UNIQUE(tenant_id, email)`, `CHECK (email ~ '^[^@]+@[^@]+\.[^@]+$')`

**Indexes**: `idx_tenant_members_tenant_id`, `idx_tenant_members_email`

### `api_keys`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| name | TEXT NOT NULL | "Slack integration script" |
| token_hash | TEXT NOT NULL UNIQUE | SHA-256 of `fk_live_<32 base62>` |
| token_prefix | TEXT NOT NULL | First 8 chars for display: `fk_live_ab12...` |
| permissions | TEXT[] NOT NULL DEFAULT '{}' | Same permission model as members |
| rate_limit_per_min | INT NOT NULL DEFAULT 1000 | Per-key rate limit override |
| last_used_at | TIMESTAMPTZ | |
| expires_at | TIMESTAMPTZ | Null = no expiry |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_api_keys_tenant_id`, `idx_api_keys_token_hash` (unique)

### `tenant_member_sessions`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | Direct RLS ownership |
| member_id | UUID NOT NULL FK → tenant_members(id) ON DELETE CASCADE | |
| refresh_hash | TEXT NOT NULL | SHA-256 of the refresh token |
| issued_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| last_used_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| revoked_at | TIMESTAMPTZ | Null = active |
| user_agent | TEXT | |
| ip | INET | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_tenant_member_sessions_member_id`, `idx_tenant_member_sessions_refresh_hash`

### `tenant_magic_links`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| member_id | UUID FK → tenant_members(id) ON DELETE CASCADE | Null if invitation (email not yet a member) |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| email | TEXT NOT NULL | The email the link was sent to |
| token_hash | TEXT NOT NULL | SHA-256 of the magic-link token |
| expires_at | TIMESTAMPTZ NOT NULL | 10 min from creation |
| consumed_at | TIMESTAMPTZ | Null = not yet consumed |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_tenant_magic_links_token_hash`, `idx_tenant_magic_links_email`

### `platform_admins` + `platform_admin_sessions` + `platform_admin_magic_links`

Same shape as the tenant equivalents, with `admin_id` instead of `member_id` and no `tenant_id`.
`platform_admins.email` is `UNIQUE`. `permissions` uses the `platform:*` namespace (10 permissions).

### `projects`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| name | TEXT NOT NULL | Human name, e.g. `Tauview Web` |
| public_key | TEXT NOT NULL UNIQUE | `pk_live_<22 base62>` |
| allowed_domains | TEXT[] NOT NULL DEFAULT '{}' | Exact or `*.domain` wildcard entries |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_projects_tenant_id`, `idx_projects_public_key`

### `widget_rules`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| name | TEXT NOT NULL | Label for this rule |
| priority | INT NOT NULL DEFAULT 0 | Higher wins on overlapping matches |
| enabled | BOOLEAN NOT NULL DEFAULT true | |
| matcher_type | matcher_type NOT NULL | `all_pages`, `url_exact`, `url_prefix`, `url_regex` |
| matcher_value | TEXT | Pattern/URL (null if `all_pages`) |
| trigger_type | trigger_type NOT NULL | `delay`, `scroll`, `exit_intent` |
| trigger_delay_seconds | INT | Used when `trigger_type='delay'` |
| trigger_scroll_percent | INT | Used when `trigger_type='scroll'` |
| can_rate | BOOLEAN NOT NULL DEFAULT true | |
| can_write_text | BOOLEAN NOT NULL DEFAULT true | |
| can_record_audio | BOOLEAN NOT NULL DEFAULT false | Plan-gated |
| can_record_video | BOOLEAN NOT NULL DEFAULT false | Plan-gated |
| can_attach_element | BOOLEAN NOT NULL DEFAULT false | HTML + computed styles |
| can_attach_screenshot | BOOLEAN NOT NULL DEFAULT false | Separate from element attach |
| greeting | TEXT | Custom message shown by the widget |
| position | widget_position NOT NULL DEFAULT 'bottom-left' | |
| theme | widget_theme NOT NULL DEFAULT 'light' | |
| accent_color | TEXT | Hex color `^#[0-9a-fA-F]{6}$`, empty = default |
| replay_mode | replay_mode NOT NULL DEFAULT 'none' | Plan-gated |
| replay_sampling_rate | INT NOT NULL DEFAULT 0 | 0-100, used when `replay_mode='sampling'` |
| replay_max_duration_seconds | INT NOT NULL DEFAULT 1800 | |
| replay_max_events | INT NOT NULL DEFAULT 50000 | |
| replay_mask_all_inputs | BOOLEAN NOT NULL DEFAULT true | rrweb `maskAllInputs` |
| replay_mask_text_selector | TEXT | CSS selector for extra masking |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_widget_rules_project_id`, `idx_widget_rules_enabled`, `idx_widget_rules_priority`

**Save-time validation**: trigger/matcher consistency, regex compiles, accent_color format,
plan-feature alignment (e.g. `can_record_audio=true` requires plan's `AudioRecording=true`).

### `feedbacks`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| widget_rule_id | UUID FK → widget_rules(id) ON DELETE SET NULL | |
| public_id | TEXT NOT NULL | `fb_<8 base62>`, `UNIQUE(tenant_id, public_id)` |
| issue_id | UUID FK → issues(id) ON DELETE SET NULL | Auto-created issue per feedback |
| session_id | UUID FK → sessions(id) ON DELETE SET NULL | Optional replay link |
| client_feedback_id | UUID NOT NULL | Widget-generated, idempotency key, `UNIQUE(project_id, client_feedback_id)` |
| rating | SMALLINT | 1-5 |
| text | TEXT | Free-form review text |
| text_cleaned | TEXT | AI-cleaned text (filler removed) |
| text_sentiment_score | DECIMAL(3,2) | -1.0 to 1.0 |
| text_moderation_flags | TEXT[] NOT NULL DEFAULT '{}' | AI moderation flags on text |
| text_process_status | processing_status NOT NULL DEFAULT 'pending' | AI pass state for text |
| text_processing_generation | BIGINT NOT NULL DEFAULT 0 | Invalidates stale retry tasks after reprocess |
| ai_processed_at | TIMESTAMPTZ | When all AI passes finished |
| ai_job_enqueued_at | TIMESTAMPTZ | For reconcile_pending_jobs |
| reporter_email | TEXT | Optional email for follow-ups |
| notify_reporter | BOOLEAN NOT NULL DEFAULT false | Opt-in for issue updates |
| page_url | TEXT NOT NULL | |
| referrer | TEXT | |
| user_agent | TEXT | |
| os | TEXT | Parsed |
| browser | TEXT | Parsed |
| device | TEXT | `desktop`, `tablet`, `mobile` |
| viewport_width | INT | |
| viewport_height | INT | |
| language | TEXT | |
| dom_snapshot_html | TEXT | HTML of attached element |
| dom_snapshot_styles | TEXT | Computed styles of attached element |
| status | feedback_status NOT NULL DEFAULT 'open' | `open`, `spam`, `archived` |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_feedbacks_tenant_id`, `idx_feedbacks_project_id`, `idx_feedbacks_issue_id`,
`idx_feedbacks_session_id`, `idx_feedbacks_public_id`, `idx_feedbacks_status`,
`idx_feedbacks_created_at`, `idx_feedbacks_client_feedback_id` (unique composite with project_id)

### `attachments`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| feedback_id | UUID NOT NULL FK → feedbacks(id) ON DELETE CASCADE | |
| kind | attachment_kind NOT NULL | `screenshot`, `audio`, `video`, `replay` |
| client_attachment_id | UUID NOT NULL | Widget-generated, `UNIQUE(feedback_id, client_attachment_id)` |
| storage_key | TEXT NOT NULL | `tenants/{tid}/feedbacks/{fid}/attachments/{aid}/{kind}.{ext}` |
| content_type | TEXT | MIME type |
| size_bytes | BIGINT | |
| duration_seconds | INT | For audio/video/replay |
| processing_status | processing_status NOT NULL DEFAULT 'pending' | Includes `skipped` |
| processing_attempts | INT NOT NULL DEFAULT 0 | |
| complete_token_hash | TEXT | Single-use completion token, cleared on complete |
| completed_at | TIMESTAMPTZ | |
| transcription | TEXT | AI-generated transcript |
| sentiment_score | DECIMAL(3,2) | -1.0 to 1.0 |
| moderation_flags | TEXT[] NOT NULL DEFAULT '{}' | e.g. `profanity`, `harassment` |
| error_message | TEXT | On failure |
| processed_at | TIMESTAMPTZ | When AI finished |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_attachments_tenant_id`, `idx_attachments_feedback_id`, `idx_attachments_kind`,
`idx_attachments_processing_status`, `idx_attachments_client_attachment_id` (unique composite)

### `ai_gemini_files`

Durable cleanup ledger for temporary Gemini File API uploads. Each row records the tenant,
attachment, remote filename, next deletion time, enqueue time, attempt count, last error, and
terminal `deleted_at` or `failed_at` timestamp. The reconciler claims due rows with
`FOR UPDATE SKIP LOCKED`; filenames remain recoverable across worker crashes and provider outages.

### `sessions`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| feedback_id | UUID FK → feedbacks(id) ON DELETE SET NULL | Primary display link |
| session_id | TEXT NOT NULL | Client-generated session uuid |
| page_url | TEXT | First page of the session |
| started_at | TIMESTAMPTZ NOT NULL | |
| ended_at | TIMESTAMPTZ | |
| events_count | INT NOT NULL DEFAULT 0 | |
| storage_key | TEXT | `tenants/{tid}/sessions/{sid}/replay.json` (set after merge) |
| rrweb_version | TEXT NOT NULL | Pinned capture library version |
| merged_at | TIMESTAMPTZ | When batch merge job finished |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_sessions_tenant_id`, `idx_sessions_project_id`, `idx_sessions_feedback_id`,
`idx_sessions_session_id`

### `issues`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| public_id | TEXT NOT NULL | `{SLUG_UPPER}-{YEAR}-{SEQ:0000}`, e.g. `TAU-2026-0001` |
| title | TEXT | Auto-set from feedback text (first 80 chars) or `[No text]` |
| status | issue_status NOT NULL DEFAULT 'open' | |
| merged_into_id | UUID FK → issues(id) ON DELETE SET NULL | Soft-merge target |
| merged_at | TIMESTAMPTZ | |
| reporter_email | TEXT | For notifications (copied from feedback) |
| notify_reporter | BOOLEAN NOT NULL DEFAULT false | |
| resolved_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_issues_tenant_id`, `idx_issues_project_id`, `idx_issues_public_id`,
`idx_issues_status`, `idx_issues_merged_into_id`

### `issue_updates`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| issue_id | UUID NOT NULL FK → issues(id) ON DELETE CASCADE | |
| member_id | UUID FK → tenant_members(id) ON DELETE SET NULL | Who made the change |
| old_status | issue_status | |
| new_status | issue_status NOT NULL | |
| comment | TEXT | Optional internal note |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_issue_updates_tenant_id`, `idx_issue_updates_issue_id`, `idx_issue_updates_created_at`

### `issue_counters`

| Column | Type | Notes |
|--------|------|-------|
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | PK part 1 |
| year | INT NOT NULL | PK part 2 |
| next_seq | INT NOT NULL DEFAULT 1 | Atomic increment via `ON CONFLICT DO UPDATE` |

**Primary key**: `(tenant_id, year)`. No additional indexes needed.

### `integrations`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| kind | integration_kind NOT NULL | `slack`, `teams`, `webhook` (no `email`) |
| name | TEXT NOT NULL | Display label |
| webhook_url | TEXT | Slack/Teams/webhook URL |
| channel | TEXT | Slack channel override |
| custom_headers_ciphertext | BYTEA | Webhook-only headers encrypted by the application key |
| webhook_secret_ciphertext | BYTEA | Recoverable application-encrypted HMAC secret |
| webhook_secret_fingerprint | TEXT | Non-secret fingerprint for support/display |
| events | event_type[] NOT NULL DEFAULT '{}' | Subscribed events |
| is_active | BOOLEAN NOT NULL DEFAULT true | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_integrations_tenant_id`, `idx_integrations_kind`, `idx_integrations_active`

### `delivery_logs`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| outbox_event_id | UUID NOT NULL FK → outbox_events(id) ON DELETE CASCADE | Stable retry identity |
| integration_id | UUID FK → integrations(id) ON DELETE SET NULL | Null for reporter emails |
| event | event_type NOT NULL | What triggered the delivery |
| resource_type | TEXT NOT NULL | `feedback` or `issue` |
| resource_id | UUID NOT NULL | |
| attempt_number | INT NOT NULL | 1, 2, 3... (from asynq retry count) |
| status | delivery_status NOT NULL DEFAULT 'pending' | `pending`, `sent`, `failed`, `bounced` |
| provider_message_id | TEXT | Resend/Slack message ID on success |
| error_message | TEXT | On failure |
| sent_at | TIMESTAMPTZ | When provider confirmed send |
| webhook_event_id | TEXT | Last verified provider webhook event id |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | When this attempt was created |

**Indexes**: `idx_delivery_logs_tenant_id`, `idx_delivery_logs_integration_id`,
`idx_delivery_logs_resource` (composite: `resource_type, resource_id`),
`idx_delivery_logs_status`, `idx_delivery_logs_created_at`

Note: one row per attempt. `bounced` is the only post-creation status change (async bounce webhook
updates a `sent` row to `bounced`).

### `outbox_events`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | Stable event/task identity |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| event | event_type NOT NULL | Domain event |
| resource_type | TEXT NOT NULL | `feedback` or `issue` |
| resource_id | UUID NOT NULL | |
| event_key | TEXT NOT NULL DEFAULT '' | Distinguishes repeated events for one resource |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | Written in the domain transaction |
| dispatched_at | TIMESTAMPTZ | Set only after all deterministic destination tasks are accepted |
| attempts | INT NOT NULL DEFAULT 0 | Dispatcher attempts |
| last_error | TEXT | Last dispatcher error |

**Constraints**: `UNIQUE(event, resource_type, resource_id, event_key)`.

The dispatcher re-fetches active destinations and uses deterministic task IDs derived from
`outbox_event_id + destination_kind + destination_id`. Duplicate dispatch is therefore harmless.

### `stripe_events`

| Column | Type | Notes |
|--------|------|-------|
| event_id | TEXT PK | `evt_...` from Stripe |
| processed_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Webhook idempotency — `INSERT ... ON CONFLICT DO NOTHING` returns 0 rows for duplicates.

### `tenant_deletions` (compliance audit, not tenant-scoped, no RLS)

| Column | Type | Notes |
|--------|------|-------|
| tenant_id | UUID PK | The deleted tenant's ID, retained |
| tenant_slug | TEXT NOT NULL | |
| tenant_name | TEXT NOT NULL | |
| requested_at | TIMESTAMPTZ NOT NULL | |
| executed_at | TIMESTAMPTZ NOT NULL | |
| requested_by_member_id | UUID | Null for platform-admin force-delete |
| forced_by_platform_admin_id | UUID | Null for self-service |
| feedback_count | INT NOT NULL | |
| attachment_count | INT NOT NULL | |

## Plans & feature flags

All plan definitions live in `backend/internal/config/plans.go` so limits and features can be
changed without touching business logic. `-1` means unlimited.

| Plan | Monthly | Yearly | Projects | Feedback/mo | Users | Storage days | AI | Replay | Slack | Video | Audio | DOM | Custom domain |
|------|---------|--------|----------|-------------|-------|--------------|----|--------|-------|-------|-------|-----|---------------|
| starter | €4.99 | 7% off | 1 | 500 | 1 | 30 | no | no | no | no | no | no | no |
| growth | €9.99 | 14% off | 4 | 2,500 | 3 | 90 | yes | limited | no | no | yes | yes | no |
| scale | €27.99 | 14% off | 10 | 10,000 | 10 | 365 | yes | yes | yes | yes | yes | yes | no |
| enterprise | custom | custom | -1 | -1 | -1 | -1 | yes | yes | yes | yes | yes | yes | yes (future) |

The `features` object returned by `GET /v1/widget/config` is derived from this table at request
time, not stored per rule. Plan overrides rule (a `starter` tenant with `replay_mode='sampling'`
gets `features.session_replay=false` and the widget ignores the setting).

## Permissions

32 tenant permissions (`resource:action`, no wildcards):

`project:create|read|update|delete`, `widget_rule:create|read|update|delete`,
`feedback:read|update|delete`, `issue:read|update|delete|merge`,
`session:read|delete`, `member:read|invite|update|remove`,
`api_key:create|read|delete`, `integration:create|read|update|delete`,
`billing:manage|read`, `tenant:update|read`.

3 UI presets (stored as explicit arrays, not roles): admin (all), editor (no deletes/member/api_key/billing), viewer (read-only).

9 platform permissions: `platform:tenant:create|read|update|suspend|delete`,
`platform:admin:create|read|update|delete`.

Enforced by `RequirePerm(perm)` middleware per route. JWT carries `permissions` snapshot
(10-min staleness accepted). API keys use the same permission model.

## Monorepo structure

```
app.fidibeki/
├── .github/workflows/      # backend-ci, widget-ci, web-ci, deploy
├── backend/                # Go module
│   ├── cmd/fidibeki-api/main.go
│   ├── internal/
│   │   ├── config/         # config.go, plans.go
│   │   ├── server/         # chi router wiring
│   │   ├── middleware/     # tenantmw, authmw, planmw, corsmw, ratelimit
│   │   ├── auth/           # magiclink, jwt, sessions, permissions.go
│   │   ├── tenant/         # TenantResolver, SubdomainResolver, CustomDomainResolver (stub)
│   │   ├── project/        # project + widget_rules CRUD
│   │   ├── feedback/       # feedback API, attachments, presign
│   │   ├── issue/          # issues, issue_updates, merge, public_id allocation
│   │   ├── ai/             # Gemini client, prompts, worker handlers
│   │   ├── email/          # Resend client, templates, worker handlers
│   │   ├── storage/        # S3/MinIO client, presign, keys.go
│   │   ├── queue/          # asynq client, server, job definitions, reconciliation
│   │   ├── stripe/         # checkout, webhooks, portal
│   │   ├── integration/    # Slack, Teams, webhook delivery
│   │   ├── platformadmin/  # platform admin auth, tenant CRUD, force-delete
│   │   ├── replay/         # session recording API, batch merge job
│   │   ├── db/             # pgxpool setup, Querier interface, migrations helper
│   │   ├── logging/        # slog setup, PII redaction, request_id
│   │   ├── api/            # errors.go (envelope + code catalog), response helpers
│   │   └── testutil/       # testcontainers, fixtures, RLS assertion helpers
│   └── Dockerfile          # multi-stage alpine
├── widget/                 # vanilla JS, esbuild, Vitest + happy-dom
│   ├── src/
│   │   ├── loader.ts       # ~5KB entry, config fetch, trigger logic
│   │   ├── widget.ts       # Shadow DOM UI
│   │   ├── triggers.ts     # delay, scroll, exit_intent
│   │   ├── config.ts       # config cache, SPA navigation detection
│   │   ├── recorder.ts     # rrweb integration, batch upload
│   │   ├── messages.ts     # error.code → user-facing strings
│   │   └── modules/        # lazy-loaded: rating, audio, video, screenshot, error-reporting
│   ├── tests/unit/         # Vitest
│   ├── tests/e2e/          # Playwright
│   └── esbuild.config.js
├── web/                    # Nuxt 3 backoffice
│   ├── pages/              # /login, /, /feedback/[id], /issues, /settings, /billing, /integrations
│   ├── components/
│   ├── composables/        # useAuth, can()
│   ├── plugins/            # apiFetch (silent refresh on 401)
│   ├── middleware/         # auth.ts (page), server middleware (SSR)
│   ├── tests/unit/         # Vitest
│   ├── tests/e2e/          # Playwright
│   ├── nuxt.config.ts
│   └── Dockerfile
├── infra/
│   ├── migrations/         # ~33 numbered SQL file pairs
│   ├── docker-compose.yml  # postgres, pgbouncer, redis, minio, caddy, api, web
│   ├── Caddyfile           # *.fidibeki.com → {api, web}; widget.fidibeki.com → static
│   ├── railway.json
│   └── stripe/fixtures/    # webhook event fixtures for tests
├── docs/
│   ├── env.md              # all env vars documented
│   ├── runbook.md          # 10 failure scenarios
│   ├── api.md              # generated API docs
│   └── architecture.md     # this plan's decisions
├── plans/fidibeki.plan.md  # this file
├── .env.example            # all env vars with defaults/placeholders
└── README.md
```

pnpm workspace for `widget/` + `web/` (shared dev deps: vitest, typescript, esbuild).

## Env var catalog

See `.env.example` for the full list (~35 vars). Key groups: App, Database (3 URLs), Redis,
S3/MinIO, Auth (JWT_SECRET + TTLs), Stripe (secret + webhook + 6 Price IDs), Resend (key + 3
from/reply addresses), Gemini (key + model + RPM), Sentry (DSN + release), Turnstile (secret +
site key), Queue (4 concurrency overrides), Widget (CDN base).

## Migration sequence by phase

**Phase 0** (auth + tenant + RLS core, 10 migrations):
`create_extensions` (placeholder), `create_roles` (with `ALTER DEFAULT PRIVILEGES`),
`create_tenants` (+RLS), `create_tenant_members` (+RLS), `create_api_keys` (+RLS),
`create_platform_admins`, `create_platform_admin_sessions`,
`create_platform_admin_magic_links`, `create_tenant_member_sessions`, `create_tenant_magic_links`.

**Phase 2** (1): `add_tenant_magic_link_invite_permissions` (persists the explicit permission
snapshot for invitations whose `member_id` remains null until the link is consumed).

**Phase 3** (3): `create_projects` (+RLS), `create_widget_enums`, `create_widget_rules` (+RLS).

**Phase 5** (10): `create_feedback_status_enum`, `create_processing_status_enum` (with `skipped`),
`create_issue_status_enum`, `create_event_type_enum`, `create_issue_counters`, `create_issues`
(+RLS), `create_feedbacks` (+RLS, includes issue_id, reporter_email, notify_reporter,
client_feedback_id, ai_job_enqueued_at), `create_attachments` (+RLS, includes client_attachment_id,
complete_token_hash, completed_at), `create_outbox_events` (+RLS), `create_delivery_status_enum`.

**Phase 6** (4): `add_feedbacks_text_ai_columns` (text_cleaned, text_sentiment_score,
text_moderation_flags, text_process_status, text_processing_generation, ai_processed_at),
`add_attachments_error_columns` (error_message, processing_attempts, terminal-state backfill),
`create_ai_daily_usage` (+RLS, atomic per-tenant/day budget), `create_ai_gemini_files`
(+RLS, durable Gemini file cleanup ledger).

**Phase 7** (2): `create_issue_updates` (+RLS), `create_delivery_logs` (+RLS, reporter email attempts).

**Phase 8** (2): `create_sessions` (+RLS, includes feedback_id, rrweb_version, merged_at),
`add_feedbacks_session_link`.

**Phase 9** (3): `create_integration_kind_enum`, `create_integrations` (+RLS, includes encrypted
webhook secret and custom headers), `add_delivery_logs_integration` (nullable integration_id FK).

**Phase 10** (3): `create_stripe_events`, `add_tenants_billing_columns` (pending_plan, email_bounced,
deletion_requested_at), `create_tenant_deletions` (compliance audit, no RLS).

Every migration is one concept, inline RLS, paired `.down.sql`, tested via up→down→up in CI.

---

## Phase 0: Foundations

**User stories covered**: As a developer, I can run the entire platform locally so that I can
develop and test end-to-end.

### What to build

Set up the monorepo, local infrastructure (Postgres + pgbouncer, MinIO, Redis, Caddy reverse
proxy), the Go module with chi, slog, Sentry init, two pgxpool instances (app + admin), the
first 10 migrations (auth + tenant + RLS), tenant subdomain middleware with transaction-local context, the
platform/tenant router split, `/healthz` + `/readyz`, and integration tests against real
testcontainers (Postgres, Redis, MinIO) with RLS positive/negative/admin tests.

### Acceptance criteria

- [x] `docker compose up` starts Postgres, pgbouncer, MinIO, Redis, Caddy, and the API.
- [x] `go test ./...` runs unit + integration tests including testcontainers Postgres.
- [x] `make test-short` runs unit tests only (no testcontainers) in <1s with a warm build cache.
- [x] `GET /healthz` returns `200 OK` with `{ "status": "ok" }`.
- [x] `GET /readyz` returns `200` if DB+Redis+S3 pass, `503` with per-check status otherwise.
- [x] Two `pgxpool` instances (app + admin) with the right roles; migrator uses direct URL.
- [x] Middleware resolves `acme.localhost` → tenant, starts `pgx.Tx`, and calls
  `SELECT set_config('app.tenant_id', $1, true)`; it rolls back on panic and flushes the buffered response only after
  commit succeeds. Unknown subdomain → `404 unknown_tenant`. Suspended → `403 tenant_suspended`.
- [x] RLS enabled on all tenant-scoped tables; integration tests verify positive (tenant reads own),
  negative (tenant can't read other tenant's), and admin bypass.
- [x] `X-Request-ID` header on every response; `slog` logs include `request_id`, `tenant_id`, `tenant_slug`.
- [x] CI runs `go vet`, `golangci-lint`, `go test` (with testcontainers), up→down→up migration test.
- [x] `.env.example` committed; `Config.Validate()` exits with clear error on missing required vars.

---

## Phase 1: Multi-tenancy & Plans

**User stories covered**: As the platform, I can identify and limit each company account so that
data stays isolated and plan features are enforced.

### What to build

Tenant management, plan configuration in `plans.go`, runtime feature/limit checks via
`CanUseFeature(tenant, feature)` and `RequirePlanLimit` middleware. Platform admin auth (magic-link,
shared library), the transactional-email asynq worker and Resend adapter required by authentication,
plus `POST /admin/tenants` CRUD. A local capture adapter makes the flow testable without Resend.
Slug validation (3-24 chars, lowercase, not reserved, unique). Cross-tenant access rejected with
`403` in integration tests. A seed command creates or promotes the first platform admin.

### Acceptance criteria

- [x] `POST /admin/tenants` creates a tenant (platform-admin auth, `platform:tenant:create` perm).
- [x] Middleware injects `(tenant, status, plan, trial_ends_at)` into request context.
- [x] Unknown subdomains return `404 unknown_tenant`; suspended tenants return `403 tenant_suspended`.
- [x] `CanUseFeature(tenant, feature)` returns false when a feature is not in the tenant's plan.
- [x] `RequirePlanLimit` middleware returns `402 plan_limit_exceeded` with `upgrade_url` on exceeded limits.
- [x] `RequirePlanLimit` returns `402 plan_feature_unavailable` when a feature isn't in the plan.
- [x] A tenant's plan limits (projects, users, feedback/month, storage days, features) returned by a config endpoint.
- [x] Cross-tenant resource access is rejected with `403` in integration tests (RLS + app-layer).
- [x] Platform admin magic-link auth flow: `POST /admin/auth/login`, `POST /admin/auth/verify`,
  `POST /admin/auth/refresh`, `POST /admin/auth/logout`.
- [x] `fidibeki-api admin bootstrap --email ...` idempotently creates the first platform admin.
- [x] Magic-link delivery works through the local capture adapter and the production Resend adapter.
- [x] `RequirePlatformAdmin(perm)` middleware on platform routes; `aud=platform` JWT enforced.
- [x] Slug validation at app + DB level (regex + reserved words check).

---

## Phase 2: Backoffice authentication

**User stories covered**: As a company user, I can log into the backoffice via magic link and
invite teammates so that I can manage feedback securely.

### What to build

Magic-link registration/login flow for tenant members (reuses the `internal/auth/magiclink`
library from Phase 1, adds `TenantStore`). JWT access + refresh tokens with `aud=tenant`.
Permission-based access control via `RequirePerm` middleware. Invitation flow for additional
users (magic link with `member_id=null` until consumed). Refresh token rotation + revocation.

### Acceptance criteria

- [x] `POST /auth/login` sends a magic link email (async via asynq `email-pool`).
- [x] `POST /auth/verify` validates the magic link token, issues access + refresh, sets refresh cookie.
- [x] `POST /auth/refresh` rotates the refresh token (revoke old, issue new); same-fingerprint concurrent
  refreshes have a 10-second idempotent grace window, while later, mismatched, or stale-successor reuse
  revokes all sessions.
- [x] `POST /auth/logout` revokes the session, clears the refresh cookie.
- [x] Admins have full permissions; custom permission sets assignable to members.
- [x] Backoffice routes require `tenant` context + `RequireTenantMember` + `RequirePerm(perm)`.
- [x] `aud=tenant` JWTs rejected on platform routes; `aud=platform` JWTs rejected on tenant routes.
- [x] Refresh tokens stored hashed; revocable per-session and per-member.
- [x] Invitations: `POST /auth/invite` sends a magic link to a non-member email; on verify, creates the member.

---

## Phase 3: Projects & widget configuration

**User stories covered**: As a product team, I can create a project, configure the widget per
page and get an embed script so that I can collect feedback on my site.

### What to build

CRUD for projects + widget_rules. Projects hold a unique public key (`pk_live_<22 base62>`)
and allowed domains (exact or `*.domain` wildcards). Widget rules stored per the revised schema
(typed columns for matcher, trigger, form fields, replay, theme). Save-time validation including
plan-feature alignment (e.g. `can_record_audio=true` requires plan's `AudioRecording`).

### Acceptance criteria

- [x] Members with `project:create`/`project:update` can manage projects and widget rules.
- [x] Each project has a unique `pk_live_` public key.
- [x] Widget rules can vary by URL pattern (`all_pages`, `url_exact`, `url_prefix`, `url_regex`).
- [x] `allowed_domains` supports exact + `*.domain` wildcard entries.
- [x] Save-time validation: trigger/matcher consistency, regex compiles, accent_color format,
  plan-feature alignment (`422` with field-level errors if misaligned).
- [x] `RequirePerm` middleware enforces `widget_rule:create/update/delete` per route.

---

## Phase 4: Widget embed

**User stories covered**: As an end user, I am asked for feedback at the right moment on a
website so that I can report confusing or helpful experiences easily.

### What to build

Zero-dependency vanilla JS widget built with esbuild into a ~5KB `loader.js` + lazy ES modules.
Shadow DOM for isolation. One-line embed (`<script src=... data-fidibeki-key=... async>`).
Config fetch from `GET /v1/widget/config` (public key + Origin check). Triggers: delay, scroll,
exit_intent with `sessionStorage` suppression. SPA navigation detection via history patch +
custom event; config refreshed every 5 min and on `visibilitychange`. Attach-element: HTML +
computed styles. Screenshot capture is independently plan/rule gated. Honeypot field for bot protection.

### Acceptance criteria

- [x] Widget loads on allowed domains and is blocked on unknown domains (`403` from config endpoint).
- [x] Trigger respects per-page config (delay, scroll depth, exit intent).
- [x] End user can rate 1-5 stars and write a text review.
- [x] End user can click a page element to attach its HTML + computed styles.
- [x] Widget sends OS, browser, viewport size, URL, referrer metadata.
- [x] Lazy modules load only for features the plan allows (`features` object from config).
- [x] Honeypot field silently drops bot submissions (returns `200` but no feedback created).
- [x] Sentry SDK lazy-loads on first error; PII scrubbed in `beforeSend`.
- [x] Vitest unit tests for trigger logic, config matching, suppression state.
- [x] Playwright E2E: open sample page → fetch config → trigger widget → serialize a submission
  against a contract-compatible mock API. The real submission/DB E2E is owned by Phase 5.

---

## Phase 5: Feedback API

**User stories covered**: As the widget, I can submit feedback and media to the platform so
that product teams see it in the backoffice.

### What to build

Public `POST /v1/feedback` on the dedicated RLS-enforced widget router (widget auth:
`X-Fidibeki-Public-Key` + Origin + rate limit). JSON metadata first → presigned S3 URLs →
completion callback. Auto-creates an issue per
feedback (transactional). Returns `{ feedback_id, issue_public_id, notify_reporter }`.
No reporter token (email-only via Q19e unsubscribe JWT). Rate-limited (10/min per IP per
project + 100/hour per project ceiling). Plan limit checked (402 if exceeded).

### Acceptance criteria

- [x] Valid project key + allowed origin required (`403` otherwise).
- [x] Monthly feedback count checked against plan; exceeded returns `402 plan_limit_exceeded`.
- [x] Rate limit: 10/min per IP per project (`429` with `Retry-After`); 100/hour per-project ceiling.
- [x] `client_feedback_id` + `client_attachment_id` provide idempotency (duplicate → returns existing).
- [x] Presigned S3 URLs returned (10-min TTL); widget PUTs directly; completion callback verifies
  token + S3 HEAD (size + MIME).
- [x] Invalid payloads return `422 validation_error` with field-level `details[]`.
- [x] Issue auto-created in the same transaction (`public_id` allocated via `issue_counters`).
- [x] Feedback and its `feedback.created` outbox event are committed atomically; the dispatcher
  uses deterministic task IDs and retries undispatched rows.
- [x] Integration tests: happy path, invalid origin, plan limit, rate limit, malformed body, idempotent retry.

---

## Phase 6: AI processing (Gemini)

**User stories covered**: As a product team, I can see transcribed and moderated feedback so
that I can triage faster.

### What to build

Per-attachment + per-text asynq jobs (`ai.attachment_process`, `ai.text_process`).
Gemini `2.5-flash` for all tasks. Stream MinIO → Gemini File API → `generateContent` (structured
JSON output) → delete Gemini file. `redis_rate` limiter keyed `gemini` (`GEMINI_RPM` env).
Completion check sets `feedbacks.ai_processed_at` when all passes done. Plan-gated: non-AI plans
get `processing_status='skipped'`. Video uses Gemini native video (no FFmpeg). Manual retry per
attachment + reprocess-all per feedback from backoffice.

### Acceptance criteria

- [x] Audio feedback transcribed when plan allows AI; `processing_status='completed'` with `transcription`.
- [x] Text feedback: cleaned (filler removed), sentiment score stored, moderation flags stored.
- [x] Screenshot OCR extracts visible text when plan allows AI.
- [x] Video: Gemini native video transcription (no FFmpeg).
- [x] Offensive language flagged in `moderation_flags`; not auto-hidden (flag-only, tenant decides).
- [x] AI jobs queued via asynq `ai-pool` (concurrency 3) with `redis_rate` Gemini limiter.
- [x] Transient failures (429, 500, timeout): revert to `pending`, asynq retries with backoff.
- [x] Permanent failures (content policy, malformed media): `processing_status='failed'`, `asynq.SkipRetry`.
- [x] `feedbacks.ai_processed_at` set when all passes finish.
- [x] Backoffice shows AI progress badge (Phase 12 dashboard dependency).
- [x] `POST /v1/attachments/{id}/retry` (re-enqueues) + `POST /v1/feedbacks/{id}/reprocess` (all).
- [x] Failures never crash the feedback submission flow (async, isolated).

---

## Phase 7: Issues & email notifications (Resend)

**User stories covered**: As an end user, I can receive email updates about the issue I reported
so that I know my feedback was acted on.

### What to build

Issues typed rows with `{SLUG}-{YEAR}-{SEQ}` public id (allocated via `issue_counters`).
Auto-created per feedback (Q20d); tenant can merge (soft-merge with `merged_into_id`).
Status workflow `open|in_progress|resolved|closed` with `issue_updates` history.
Reporter email notifications on issue status changes, sent via Resend `email-pool` async.
`delivery_logs` one row per attempt. Unsubscribe via JWT (`aud=reporter-unsubscribe`, 90d).
Bounce/complaint webhook auto-unsubscribes.

### Acceptance criteria

- [x] Each issue gets a public id like `TAU-2026-0001` (uppercase slug, 4-digit seq, yearly reset).
- [x] Auto-created per feedback (title = first 80 chars of text or `[No text]`).
- [x] Tenant can merge issues (soft-merge, `merged_into_id` preserved, un-merge possible).
- [x] Reporter email optional; if provided + opted-in, status updates trigger an email.
- [x] Emails sent from `updates@fidibeki.com` with `{Tenant name} via Fidibeki` from-name.
- [x] Unsubscribe link (JWT, 90d) in every reporter email; clicking sets `notify_reporter=false`.
- [x] `delivery_logs` records each attempt with `attempt_number`, `status`, `provider_message_id`.
- [x] Resend bounce/complaint webhook on `api.fidibeki.com`: updates `delivery_logs.status='bounced'`,
  auto-unsubscribes reporter on complaint.
- [x] `issue_updates` records every status change with `member_id`, `old_status`, `new_status`, `comment`.

---

## Phase 8: Session replay

**User stories covered**: As a product team, I can replay a user's session so that I can
understand exactly what confused them.

### What to build

rrweb (pinned) with PII masking. Session-level sampling + caps (30 min, 50k events).
Batch upload to S3 presigned PUT (~200KB per batch); post-session `replay.merge` job concatenates
into `replay.json` and deletes batches. `rrweb-player` in a Vue component; replay streamed
through Go API (auth + RLS, not direct S3). `sessions.rrweb_version` + `merged_at` columns.
Retention = plan's `storage_days` (lifecycle + daily DB cleanup cron).

### Acceptance criteria

- [x] Replay recording respects tenant sampling config (0%, 1%, 100%, etc.).
- [x] PII masking: `maskAllInputs=true`, `maskTextSelector` for extra fields.
- [x] Recording continuous across SPA navigations; ends on `pagehide` or 5-min idle.
- [x] Batch upload (one S3 object per ~50 events); `replay.merge` job produces final `replay.json`.
- [x] Backoffice shows "processing" until merge, then "ready"; player loads concatenated events.
- [x] Replay linked to feedback (both `sessions.feedback_id` and `feedbacks.session_id`).
- [x] Storage respects plan retention days (lifecycle policy + daily cleanup cron).
- [x] Feature blocked for plans without `SessionReplay=true` (`402 plan_feature_unavailable`).

---

## Phase 9: Slack integration

**User stories covered**: As a product team, I can receive feedback notifications in Slack so
that my team reacts immediately.

### What to build

Integrations in the generic `integrations` table (Slack/Teams/webhook, no email kind).
Per-integration HMAC webhook secret. Event dispatch: on `feedback.created` etc., enqueue
one `integration.deliver` job per active integration subscribed to the event. Slack Block Kit
messages. Webhooks signed with `X-Fidibeki-Signature: HMAC-SHA256`. Teams MessageCard for MVP.
`delivery_logs` per attempt. Transactional outbox dispatch + deterministic task identities.

### Acceptance criteria

- [x] Admins can save/remove integrations and select event types (`integration:create/delete` perm).
- [x] Webhook secret generated on creation, shown once, stored application-encrypted with a
  fingerprint; `X-Fidibeki-Signature` header on every webhook.
- [x] New feedback sends a Slack message (Block Kit) when configured for `feedback.created`.
- [x] Slack/Teams/webhook failures retried per asynq policy; logged in `delivery_logs`; don't block feedback flow.
- [x] One asynq job per integration (failure isolation); reporter email is a separate job.
- [x] Transactional outbox rows fan out to deterministic per-integration tasks; partial enqueue
  retries do not duplicate provider deliveries.
- [x] Feature only available for plans with `SlackNotifications=true` (`402` otherwise).

---

## Phase 10: Stripe billing

**User stories covered**: As a company, I can start a free trial, choose a plan and pay so
that the platform bills me automatically. I can also close my account.

### What to build

Self-service signup (`POST api.fidibeki.com/v1/signup`, Turnstile-protected). Stripe Checkout
(hosted) + Customer + Subscription + one Price per plan. Hybrid trial (Stripe + Fidibeki
`trial_ends_at` + daily cron + Resend dunning). 6 webhook events + `stripe_events` idempotency.
Downgrades: restrict-new-creation + `pending_plan` applied at next period. Customer Portal.
Account deletion: self-service with 7-day grace + platform admin force-delete +
`tenant_deletions` audit table.

### Acceptance criteria

- [x] `POST /v1/signup` creates a `trial` tenant + first admin member; sends magic link; Turnstile verified.
- [x] New tenant starts a 14-day trial with the chosen plan's limits.
- [x] Lazy Stripe customer creation on first billing interaction.
- [x] Stripe checkout creates a subscription and links it to the tenant.
- [x] Stripe Checkout success/cancel top-level returns preserve the tenant session; staging cancel
  verification returned to billing while authenticated without creating a subscription or plan change.
- [x] Webhooks update plan, status, trial end date (`stripe_events` idempotency; load-from-Stripe).
- [x] Dunning emails at 3d/1d/0d before trial end (Resend async).
- [x] Unpaid trial converts to suspended; feedback API blocks suspended tenants.
- [x] Downgrades allowed; new resource creation blocked over limit (`402`); existing resources fully usable.
- [x] `pending_plan` applied at next billing period via `invoice.paid` webhook.
- [x] Customer portal link available for admins (`POST /billing/portal`).
- [x] `POST /v1/billing/delete-account` (typed confirmation, 7-day grace, email to members).
- [x] `POST /v1/billing/cancel-deletion` (during grace).
- [x] `DELETE /admin/tenants/{id}` (platform admin force-delete, immediate, audited).
- [x] Daily `tenant_deletion_executor` cron: cancel Stripe, revoke sessions, delete S3, `DELETE FROM tenants CASCADE`, audit log.
- [x] Delete unverified tenants (no magic-link consumption) after 24h.

---

## Phase 11: Backoffice Nuxt UI

**User stories covered**: As a company user, I can manage projects, view feedback, triage
issues and configure the widget in a branded backoffice.

### What to build

Nuxt 3 on `*.fidibeki.com`, hybrid SSR (`/login` + `/` SSR, rest SPA). `apiFetch` plugin with
silent-refresh-on-401. `useState` for auth. `can(perm)` for authz (plan limits surface as 402).
Pages: dashboard (metrics), project list, widget config editor, feedback list (cursor-paginated
+ 9 filters), feedback detail (metadata, screenshot, transcript, sentiment, replay player),
issue detail (status updates, merge), settings (members, api_keys, integrations), billing
(plan, portal, delete-account). No per-tenant branding for MVP (tenant name in header).

### Acceptance criteria

- [x] Login page per subdomain sends magic link; `GET /auth/verify` (SSR) → `POST /auth/verify` (Go).
- [x] Dashboard shows feedback count, issue count, satisfaction trend.
- [x] Members with correct permissions can manage projects and widget configuration.
- [x] Feedback list: cursor pagination + filters (project, status, rating, attachment, moderation, issue, date, sort).
- [x] Feedback detail: metadata, screenshot, transcript, sentiment, replay player (`rrweb-player`).
- [x] Issues can be created/updated/resolved/merged and trigger emails.
- [x] "Retry AI" per attachment + "Reprocess all" per feedback.
- [x] Settings: members (invite, permissions editor with 3 presets), API keys, integrations.
- [x] Billing: plan, customer portal link, delete-account with typed confirmation + grace banner.
- [x] Trial banner ("14-day trial") shown during trial.
- [x] Playwright E2E: login, project creation, feedback triage, issue merge, billing delete-request.

---

## Phase 12: Deploy & observability

**User stories covered**: As the platform, I run reliably in production with logs, CI/CD and
staging parity.

### What to build

Multi-stage alpine Dockerfiles (Go + Nuxt separate). Widget served as static files by Caddy/CDN.
Railway `railway.json` + pgbouncer config. GitHub Actions: PR pipeline (<8 min, fast + integration),
main pipeline (auto-deploy staging + post-deploy E2E + manual promote to prod). Migrations run
in deploy (`migrate up` via `DATABASE_URL_DIRECT`). Source maps uploaded to Sentry in CI.
`docs/env.md` + `docs/runbook.md` (10 scenarios). Data export tool (deferred — manual via runbook for now).

### Acceptance criteria

- [x] `docker build` produces runnable API and web images (alpine, multi-stage).
- [x] Widget built to `widget/dist/` (esbuild, content-hashed filenames, long Cache-Control).
- [x] Railway deploys `main` to staging automatically; prod is manual promote.
- [x] Migrations run before app start (`migrate up` via direct URL); failure = old instance stays live.
- [x] All env vars documented in `docs/env.md` + `.env.example`.
- [x] Healthcheck and DB connectivity verified on deploy (`/readyz`).
- [x] Railway staging bucket CORS supports browser presigned `PUT`; real widget screenshot, audio,
  and replay uploads completed, Gemini OCR completed, replay merge completed, and masked playback rendered.
- [x] Source-map upload is integrated into CI (Go release + widget + Nuxt); credentials gate execution.
- [x] `docs/runbook.md` covers: DB migration failure, Stripe webhook replay, Redis queue backup,
  AI processing failures, email delivery failures, tenant suspended incorrectly, widget not loading,
  S3 storage full, deploy rollback, secrets rotation.
- [x] Read-only Asynq queue operations UI mounted on `api.fidibeki.com` behind platform-admin auth.

Local packaging and deployment automation are complete. Railway staging is configured and verified;
the first production promotion remains intentionally manual.

### Production launch gates

- [x] Store an environment-scoped Railway production token in GitHub Actions.
- [ ] Provision isolated production Postgres, Redis, object storage, API, worker, web, and widget services;
  configure the production bucket's browser-upload CORS policy.
- [ ] Configure production-only runtime credentials for Resend, Stripe, Turnstile, Gemini, and Sentry.
- [ ] Configure and validate `api.fidibeki.com`, `*.fidibeki.com`, and `widget.fidibeki.com` DNS and TLS.
- [ ] Enable data backups and deployment alerts; set the intended production replica counts.
- [ ] Run the manually confirmed production promotion and complete production smoke tests.
