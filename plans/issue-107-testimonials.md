# Plan: Turn curated feedback into embeddable testimonials (issue #107)

> Source: https://github.com/fidibeki/app.fidibeki/issues/107
> Scope: candidate scoring, consent-gated collection, a testimonial library with tags,
> customer-curated walls, a Web Component + public JSON embed, and native video.
> Not in scope: iframe embed, OG images, third-party review import, wall A/B testing.

## Product decisions of record

| Decision | Choice | Consequence for this design |
| --- | --- | --- |
| Consent | Author opt-in is **mandatory** | Nothing publishes without an author-granted, revocable consent record |
| Embed | **Web Component + public JSON API** | One snapshot payload serves both; no iframe resize handshake |
| Media | Text + photo + **native video** | Dedicated transcode worker, immutable public media path |
| Wall scope | **Tenant-level**, optional project filter | Walls live beside `tags`, not beside `widget_rules` |
| Curation | Platform **suggests**, customer **selects** | Scoring feeds a ranked inbox; wall membership is always explicit |

## Shape

Reuse what exists instead of inventing:

- **Tags already exist.** `tags` + `feedback_tags` + `issue_tags` (migration `000046`), tenant-scoped
  with colour validation and a `/v1/tags` API. Testimonials get a `testimonial_tags` join table
  against the *same* catalogue — one vocabulary across feedback, issues and testimonials.
- **The scoring signals already exist.** `ai.Result` computes `sentiment_score`,
  `moderation_flags` (incl. `pii`, `profanity`), `actionability`, `category`, `priority`,
  `cleaned_text` and `suggested_title` per feedback. Ranking is a deterministic function over data
  we already paid for.
- **The public-token page pattern already exists.** `web/pages/reporter/[token].vue` +
  `reporter_access_tokens` (SHA-256 `token_hash`, 64 chars). The collection form is the same shape.
- **The public-endpoint-under-RLS pattern already exists.** `widgetanalytics/public_http.go`
  resolves the tenant from a public identifier, then `set_config('app.tenant_id', $1, true)` inside
  the transaction so unauthenticated reads still run under row-level security. Every public
  testimonial endpoint follows this exactly.
- **The asset pipeline already exists.** `widget/esbuild.config.mjs` emits hashed ESM entries plus a
  `manifest.json`, and `infra/Caddyfile` already serves `widget.localhost` with
  `Access-Control-Allow-Origin: *`, `Cross-Origin-Resource-Policy: cross-origin` and immutable
  caching. The embed is a new entry point, not new infrastructure.
- **Media validation already exists.** `feedback/media_validation.go` structurally validates
  `video/webm` and `video/mp4` (mp4ff), and `attachment-verification-pool` already runs safety
  scanning with `AttachmentSafetyStatus`.
- **Consent evidence already has a shape.** `ReporterConsentInput` /
  `ReporterConsentNoticeSnapshot` carry `notice_version`, `controller_profile_version`,
  `widget_definition_version` and `locale`. Testimonial consent extends the same idea.

Two things are deliberately *new*, because nothing existing fits:

1. **A published snapshot table.** The public embed must not join six RLS-protected tables per
   request. Publishing materialises an immutable JSON payload with a strong ETag.
2. **A dedicated media worker.** ffmpeg is ~100 MB and CPU-heavy; putting it in the shared `worker`
   image would couple email delivery latency to video transcoding.

---

## Domain model

### The central distinction

A testimonial is **not** a feedback with a flag. It is a separate aggregate with its own lifecycle,
its own consent record, its own media and its own publication state. It *originates* from a
feedback (or from a standalone collection form) and then lives independently — the author may
rewrite it, add a video, and later revoke it, none of which should mutate the original feedback.

### Two orthogonal state axes

Conflating editorial status with consent status is the classic failure mode of this feature: a
customer "approves" a testimonial whose author revoked consent yesterday, and it goes live. So:

```
status         : draft → pending_review → approved → archived
consent_state  : pending → granted → pending_reconsent → revoked | expired
```

A testimonial is publishable **iff**
`status = 'approved' AND consent_state = 'granted' AND every referenced media is safety-clean and transcoded`.
This predicate lives in one function, is unit-tested exhaustively, and is the only thing the
snapshot builder consults.

### The edit rule

The author consents to *specific words*. If the customer then rewrites the quote, the consent no
longer covers what is published. The rule:

> The published `quote` must be a **contiguous substring** of `consented_text` after Unicode
> normalisation and whitespace collapsing. Shortening is free. Anything else flips
> `consent_state` to `pending_reconsent` and triggers a re-confirmation email.

This lets customers trim a rambling quote (the common, legitimate case) without a round trip, while
making it structurally impossible to put words in someone's mouth. It is a pure function, so it is
cheap to test to death.

### Granular consent scopes

One boolean is not enough — people will happily publish their words but not their face. Consent is
stored as scopes:

```json
{ "quote": true, "identity": true, "company": true, "avatar": false, "video": true }
```

The snapshot builder renders only consented scopes. Revoking `avatar` alone does not unpublish the
quote.

---

## Data model

Migrations `000116`–`000120`. **Numbering caution:** worktrees share one migration number space —
re-check `git ls-tree origin/main infra/migrations/` before claiming numbers, and use an isolated
Compose project (`-p fidibeki-testimonials`) for local runs.

Every table follows the house pattern: `tenant_id` first, composite
`UNIQUE (tenant_id, id)` for composite FKs, `set_updated_at` trigger,
`ENABLE`/`FORCE ROW LEVEL SECURITY`, a `*_tenant_isolation` policy on `fidibeki_app`, and
`GRANT … TO fidibeki_app, fidibeki_admin`.

**Circular reference:** `testimonials.request_id` points at `testimonial_requests`, and
`testimonial_requests.testimonial_id` points back. Neither table can be created with both
constraints in place. `000117` creates `testimonials` with `request_id` as a plain column;
`000118` creates `testimonial_requests` and then adds *both* foreign keys via `ALTER TABLE`. The
corresponding `.down.sql` drops them in reverse.

### `000116_add_testimonial_candidates`

`testimonial_candidates` — one row per scored feedback, upserted by a worker.

- `tenant_id`, `project_id`, `feedback_id` (PK `(tenant_id, feedback_id)`)
- `base_score numeric(6,5)` — 0..1, **recency decay is applied at read time**, not stored, so a
  nightly full rescan is unnecessary
- `score_version smallint` — bump to force a rescore; old rows are recomputed lazily
- `signals jsonb` — every input with its contribution, for explainability and debugging
- `reasons text[]` — i18n keys, e.g. `testimonials.reason.concreteOutcome`
- `eligibility text` — `eligible | ineligible`
- `ineligible_reasons text[]` — hard-gate failures
- `suggested_excerpt text`, `suggested_headline text`, `suggested_tag_ids uuid[]` — AI enrichment,
  nullable; the row is fully useful without them
- `dismissed_at`, `dismissed_by` — "never suggest this again"
- `computed_at`
- Indexes: `(tenant_id, eligibility, base_score DESC) WHERE dismissed_at IS NULL`,
  `(tenant_id, project_id)`

### `000117_add_testimonials`

`testimonials`

- `id`, `tenant_id`, `public_id text` — `CHECK (public_id ~ '^ts_[A-Za-z0-9]{8}$')`, matching the
  `fb_`/`is_` convention
- `project_id uuid NULL`, `source_feedback_id uuid NULL`, `request_id uuid NULL`
- `source text` — `feedback | form | manual`
- content: `consented_text text NOT NULL` (immutable), `quote text NOT NULL`, `headline text NULL`
- author: `author_name`, `author_role`, `author_company`, `author_company_url`, `author_locale`

Note there are deliberately **no** `avatar_media_id` / `video_media_id` columns. Media points at the
testimonial, not the other way round, so there is no circular foreign key and no window in which the
two sides disagree. "At most one avatar and one video" is enforced where it belongs, by
`UNIQUE (tenant_id, testimonial_id, role)` on `testimonial_media`.
- `rating smallint CHECK (rating IS NULL OR rating BETWEEN 1 AND 5)`
- `status text` — `draft | pending_review | approved | archived`
- `consent_state text` — `pending | granted | pending_reconsent | revoked | expired`
- consent evidence: `consent_scopes jsonb NOT NULL`, `consent_notice_version text`,
  `consent_controller_profile_version int`, `consent_locale text`, `consent_granted_at`,
  `consent_evidence jsonb` (UA family, coarse region, notice snapshot hash — **no raw IP**)
- revocation: `revocation_token_hash text` (the author's self-serve revoke link, 64-char SHA-256),
  `revoked_at`, `revocation_reason text`
- `collected_at`, `approved_at`, `approved_by`, `archived_at`
- Constraints: `UNIQUE (tenant_id, id)`, `UNIQUE (tenant_id, public_id)`,
  `CHECK (consent_state <> 'granted' OR consent_granted_at IS NOT NULL)`,
  `CHECK (char_length(quote) BETWEEN 1 AND 2000)`
- `UNIQUE (tenant_id, source_feedback_id) WHERE source_feedback_id IS NOT NULL AND status <> 'archived'`
  — one *live* testimonial per feedback. The partial predicate matters: if an author revokes and the
  customer archives the result, a fresh request for the same feedback must still be possible.

`testimonial_tags` — `(tenant_id, testimonial_id, tag_id)`, FKs into `testimonials` and the
existing `tags`, mirroring `feedback_tags` exactly.

### `000118_add_testimonial_requests`

`testimonial_requests` — the invitation to expand a feedback into a testimonial.

- `tenant_id`, `id`, `project_id`, `feedback_id NULL`, `testimonial_id NULL`
- `token_hash text NOT NULL CHECK (char_length(token_hash) = 64)`, `UNIQUE`
- `recipient_email text NOT NULL`
- `status text` — `pending | opened | submitted | expired | cancelled`
- `prompt text NULL` — the customer's personal message
- `requested_fields jsonb` — which fields the form asks for
- `locale text`, `expires_at`, `sent_at`, `opened_at`, `submitted_at`,
  `reminder_count smallint NOT NULL DEFAULT 0`, `created_by`
- Index `(tenant_id, status, expires_at)`

`testimonial_forms` — a reusable public collection link, not tied to any feedback.

- `tenant_id`, `id`, `project_id NULL`, `public_id text` (`^tf_[A-Za-z0-9]{8}$`)
- `name`, `prompt`, `requested_fields jsonb`, `locale`
- `is_enabled boolean NOT NULL DEFAULT false`, `submission_limit_per_day int`
- Submissions land as `testimonials` with `source = 'form'` and `status = 'pending_review'`.

### `000119_add_testimonial_media`

`testimonial_media` — the logical asset.

- `tenant_id`, `id`, `testimonial_id NULL` (nullable: uploaded during collection, before the
  testimonial row is finalised), `request_id NULL`
- `role text` — `avatar | video`
- `source_storage_key text`, `content_type`, `size_bytes`, `duration_seconds`, `width`, `height`
- `safety_status text` — reuses the `AttachmentSafetyStatus` vocabulary
  (`pending|cleaning|quarantined|safe|rejected|expired`)
- `transcode_status text` — `pending | processing | ready | failed`, `transcode_error text`,
  `transcode_attempts smallint`
- `content_hash text` — content-addressed, drives the immutable public URL
- `complete_token_hash text` — mirrors the existing `attachments` upload-completion pattern

`testimonial_media_renditions` — the derived files.

- `tenant_id`, `id`, `media_id`, `kind text`
  (`mp4_720p | webm_720p | poster | thumbnail | captions_vtt`)
- `storage_key`, `content_type`, `size_bytes`, `width`, `height`, `content_hash`
- `UNIQUE (tenant_id, media_id, kind)` — also what makes transcode retries idempotent

`testimonial_media` additionally carries `UNIQUE (tenant_id, testimonial_id, role) WHERE
testimonial_id IS NOT NULL`, which is the single enforcement point for "one avatar, one video".

Two tables rather than one self-referencing table: the embed payload needs "one video, N
renditions" as a natural shape, and it keeps `safety_status`/`transcode_status` on the asset where
they belong rather than duplicated per file.

### `000120_add_testimonial_walls`

`testimonial_walls`

- `tenant_id`, `id`, `public_id text` (`^wl_[A-Za-z0-9]{8}$`), `name`, `slug`
- `project_id uuid NULL` — optional display filter, not an ownership boundary
- `layout text` — `grid | carousel | list | masonry | single`
- `theme jsonb` — accent, radius, font stack, `auto|light|dark`
- display toggles: `show_rating`, `show_avatar`, `show_company`, `show_video`, `max_items`
- `allowed_origins text[]` — advisory referrer allowlist (see the security note below)
- `status text` — `draft | published`, `published_at`, `created_by`

`testimonial_wall_items` — **explicit** membership and ordering.

- `(tenant_id, wall_id, testimonial_id)` PK, `position int NOT NULL`, `pinned boolean`
- `UNIQUE (tenant_id, wall_id, position) DEFERRABLE INITIALLY DEFERRED` so a drag-reorder can
  renumber inside one statement without transient collisions

`testimonial_wall_snapshots` — the immutable published payload.

- `(tenant_id, wall_id, version)` PK, `payload jsonb NOT NULL`, `etag text NOT NULL`,
  `published_at`, `built_by`
- A partial unique index keeps exactly one `is_current` row per wall.
- Retaining old versions is what makes "unpublish"/rollback trivial and gives us an audit trail of
  what was publicly visible when — which matters if consent is ever disputed.

`testimonial_wall_daily_views` — `(tenant_id, wall_id, day)` with `impressions bigint`. A single
counter, incremented from the embed's fire-and-forget beacon. Deliberately minimal.

---

## Scoring engine (`backend/internal/testimonial/scoring.go`)

A pure, versioned, deterministic function. No I/O, no LLM call, no database — so it is
table-driven-testable and cannot regress silently.

```go
func Score(in Input, version int) Assessment
```

### Hard gates (produce `ineligible`, never suggested)

- `feedback.status` is `spam` or `archived`
- moderation flags intersect `{pii, hate, harassment, sexual, violence, self_harm}`
- no text, or text shorter than 40 characters
- a testimonial already exists for this feedback, or the candidate was dismissed
- detected language is not among the tenant's supported locales

The PII gate is the important one: a feedback whose text contains an email address or an order
number must never be *suggested* for publication, no matter how glowing.

### Weighted signals

| Signal | Rationale | Weight |
| --- | --- | --- |
| `rating` 4–5 | Explicit satisfaction | 0.25 |
| `sentiment_score` (0..1 half of the AI range) | Tone independent of the star | 0.20 |
| Length in the 120–600 char band | "nice" is worthless; an essay does not fit a card | 0.15 |
| Specificity — numbers, percentages, time units, comparatives | "cut onboarding from 3 days to 20 minutes" is the whole game | 0.20 |
| `actionability = 'not_actionable'` | Actionable feedback is a bug report, not praise | 0.08 |
| Author reachable (`reporter_email` present + `notify_reporter`) | A candidate we cannot ask is worth less in practice | 0.10 |
| `profanity` flag | Penalty, not a gate | −0.12 |

Sum, clamp to `[0,1]`, store as `base_score`. The read query applies exponential recency decay
(`base_score * exp(-age_days / 90.0)`) so ranking freshens daily with no rescan.

`reasons` returns the top three positive contributors as i18n keys, so the UI can tell the customer
*why* — "5 stars", "mentions a concrete outcome", "author can be contacted". An unexplained score
is a score nobody trusts.

### AI enrichment (optional, budget-gated)

Only for the top N eligible candidates per tenant per day, dispatched on the existing `ai-pool`
under the existing daily-budget guard (`ai_daily_budget_exhausted` already exists as a deferral
reason). It produces `suggested_excerpt`, `suggested_headline` and `suggested_tag_ids`.

**Constraint:** `suggested_excerpt` is validated to be a contiguous substring of the original text
before being stored — the same rule as the edit guard. An LLM that paraphrases gets its output
rejected, not published.

If the budget is exhausted or Gemini is unavailable, candidates still rank and still work. The AI
is an accelerant, never a dependency.

### Trigger

- On `ai.text_process` completion for a feedback → enqueue `testimonial.score` on the existing
  `domain-pool` via the outbox.
- On feedback status change → rescore or invalidate.
- A `score_version` bump lazily recomputes rows on read when `score_version < current`.

---

## Collection flow

```
candidate ──customer clicks "Request testimonial"──▶ testimonial_request (pending)
                                                            │ email via email-pool
                                                            ▼
                                              /testimonials/collect/{token}
                                                            │ author submits + consents
                                                            ▼
                                         testimonial (pending_review, consent granted)
                                                            │ customer approves
                                                            ▼
                                              added to a wall, wall published
```

### The public form (`web/pages/testimonials/collect/[token].vue`)

Modelled directly on `web/pages/reporter/[token].vue`: `definePageMeta({ layout: false })`,
`meta: [{ name: 'robots', content: 'noindex,nofollow' }]`, `$apiFetch`, `useLocalizedAPIError`,
`useRegionalFormat`, and the design-system classes described below.

Four steps, each individually skippable except the last:

1. **Your words** — pre-filled with the AI-suggested excerpt (or the original text), with the full
   original shown for reference. The author may edit freely; whatever they submit *becomes*
   `consented_text`.
2. **About you** — name, role, company, company URL, photo. All optional. The form states plainly
   which fields will be public.
3. **Add a video** *(optional)* — record in-browser or upload, ≤ 90 s, ≤ 50 MB.
4. **Permission** — one checkbox per consent scope, not a single blanket tick, plus the notice
   snapshot (who the controller is, where it will appear, how long, how to revoke) and the
   revocation link, which is also emailed.

### Token handling

- 32 bytes of `crypto/rand`, base64url in the URL, only the SHA-256 hex stored (`token_hash`,
  64-char CHECK) — identical to `reporter_access_tokens` and `tenant_magic_links`.
- Constant-time comparison; lookup by hash, never by prefix.
- `expires_at` default 30 days; expiry is enforced in the query, not in Go.
- Rate limited per token and per IP via the existing `ratelimit` package.
- Honeypot field (`company_website`, the same name the widget already uses) plus optional Turnstile,
  which already exists for billing.
- Opening the link is idempotent; submitting is single-use and guarded by a unique constraint, so a
  double-click cannot create two testimonials.

---

## Video pipeline

The heaviest part of v1. Five stages, each independently retryable.

1. **Upload** — the browser requests a presigned PUT (`storage.PresignPut`), uploads directly to
   S3/MinIO, then calls a completion endpoint with `complete_token_hash`, mirroring the existing
   `attachments` flow exactly. The API never proxies the upload body.
2. **Verify** — on `attachment-verification-pool`: structural validation via the existing
   `validWebMReader` / `validMP4Reader` (never trust `Content-Type`), duration and dimension
   limits, then safety scanning into `safety_status`. A `quarantined` or `rejected` asset can never
   reach a snapshot.
3. **Transcode** — a **new `testimonial-media-pool` queue served by a new `media-worker`
   container**, so ffmpeg's image size and CPU burst never delay email or AI work. Renditions:
   - `mp4_720p` — H.264 baseline + AAC, `-movflags +faststart` (mandatory for progressive playback)
   - `webm_720p` — VP9 + Opus, best-effort; failure here is not fatal
   - `poster` — JPEG at ~1 s
   - `thumbnail` — 320 px wide
   Idempotent by `(media_id, kind)`; retries are safe. Bounded attempts, then `failed` with an
   operator-visible error.
4. **Captions** — drafted by reusing the existing Gemini transcription (`ai.Result.Transcription`
   already exists for audio), stored as `captions_vtt`, and **editable by the customer before
   publish**. The embed ships a `<track>` only when captions are `ready`. Auto-captions that nobody
   reviewed are an accessibility liability, so they are a draft, not a fact.
5. **Deliver** — see below.

### Public media delivery — and the trap to avoid

**Never put presigned URLs inside a cacheable snapshot.** A presigned URL expires; the snapshot
does not. Two hours later the wall shows broken images and nobody knows why.

Instead: a content-addressed public path served by the API.

```
GET /v1/public/media/{mediaPublicID}/{contentHash}.{ext}
```

- `Cache-Control: public, max-age=31536000, immutable` — safe because the hash *is* the identity
- `Cross-Origin-Resource-Policy: cross-origin`, `X-Content-Type-Options: nosniff`
- `Accept-Ranges: bytes` with 206 support, required for video seeking
- Resolves the tenant from the media public ID, then reads under RLS via
  `set_config('app.tenant_id', …, true)`, exactly like `widgetanalytics/public_http.go`
- Returns 404 the moment consent is revoked or safety status regresses

---

## Public embed

### Endpoints (on the existing widget host)

```
GET /v1/public/walls/{wallPublicID}          → snapshot JSON, strong ETag
GET /v1/public/media/{mediaPublicID}/{hash}  → immutable media bytes
POST /v1/public/walls/{wallPublicID}/views   → fire-and-forget impression beacon
```

Cache policy on the snapshot: `public, max-age=60, stale-while-revalidate=300`. The 60 s ceiling is
a deliberate legal choice — it bounds worst-case exposure after a consent revocation to one minute,
without giving up caching.

The impression beacon is an unauthenticated public endpoint, so its numbers are **approximate by
construction** — anyone can inflate them. It is rate-limited per wall and per IP, and the UI labels
it as an estimate. Presenting it as an exact analytics figure would be misleading.

`allowed_origins` is enforced as a **referrer allowlist, and documented as advisory only**. Wall
data is public by definition; anyone can curl the endpoint. Presenting origin checks as a security
control would be a lie. They exist to stop casual reuse, nothing more.

### Snapshot building

Publishing a wall walks `testimonial_wall_items` in `position` order, filters through the
publishable predicate, projects each testimonial through its `consent_scopes`, resolves media to
immutable URLs, and writes one `testimonial_wall_snapshots` row with a strong ETag
(SHA-256 of the payload).

Rebuild triggers:

- customer publishes / reorders / changes theme → rebuild the affected wall
- testimonial approved, archived, edited, or re-tagged → rebuild every wall containing it
- **consent revoked → rebuild immediately and synchronously in the revoking transaction**, then
  purge media. This one is not allowed to sit in a queue.
- media transcode completes → rebuild

### The Web Component

New esbuild entry points in `widget/`, reusing the existing hashed-manifest machinery:

- `widget/src/testimonials/loader.ts` → `dist/v1/testimonials.js` (IIFE, registers the element)
- `widget/src/testimonials/wall.ts` → the ESM implementation, code-split

Customer integration:

```html
<script async src="https://widget.fidibeki.com/v1/testimonials.js"></script>
<fidibeki-wall wall="wl_ab12cd34"></fidibeki-wall>
```

- Shadow DOM for style isolation; the host page cannot leak styles in and we cannot leak out.
- Theming through CSS custom properties on the host element plus `::part()` on every meaningful
  node, so a customer can restyle without forking.
- Semantic markup: `<blockquote>` + `<cite>`, real headings, `<video>` with `preload="none"`,
  `playsinline`, a poster, and no autoplay. Carousel autoplay respects
  `prefers-reduced-motion` and stops on focus.
- Zero runtime dependencies; the whole thing must stay small enough to justify the `async` tag.
- Degrades to nothing visible (not a broken box) if the fetch fails.

### The headless path

`GET /v1/public/walls/{id}` returns the same payload the component consumes, so a customer on
Next.js/Astro/Hugo can render it server-side. This is the SEO answer: Shadow DOM content is
generally indexable now, but "generally" is not something to bet a customer's social proof on.

We will emit optional `Review` JSON-LD, with a documented caveat that Google restricts rich results
for self-serving reviews on your own site — so it is offered as opt-in, not sold as an SEO feature.

---

## Backoffice

### Design system — the constraint, stated plainly

The app has **no component library and no utility framework**. It is hand-written CSS with a token
layer in `web/assets/css/main.css` and app chrome in `web/assets/css/app.css`. Everything new is
composed from the existing primitives, and **no new colour values are introduced** — light/dark
comes free from the token layer only if we stay inside it.

Primitives to use, not re-create:

| Need | Existing class |
| --- | --- |
| Page header | `.app-page-head`, `.page-heading`, `.app-eyebrow` |
| Card / panel | `.app-surface` |
| Buttons | `.app-button`, `.app-button--primary`, `.secondary-button`, `.quiet-button`, `.danger-button`, `.icon-button` |
| Inputs | `.field`, `.select-field`, `.app-input`, `.app-select` |
| Status | `.status-pill` + `.status-{pending,ready,rejected,completed,…}` |
| Tags / filters | `.tag`, `.filter-chip` |
| Tables | `.data-table` |
| Modal | `.app-modal`, `.app-modal__body` (labels are 10 px, 700, uppercase, `display: grid; gap: 6px`) |
| Empty / loading / error | `.app-state`, `.app-spinner`, `.loading-bars` |
| Metrics | `.metric-value`, `.section-label` |

New statuses map onto the existing colour semantics rather than adding any:
`pending_review → .status-pending` (blue), `approved → .status-ready` (green),
`revoked → .status-rejected` (red), `archived → .status-deferred` (amber).

Accessibility and i18n are not optional here: focus rings come from the token layer, all copy goes
into `web/i18n/locales/{en,pt,pt-PT,pt-AO}.ts`, and the `en-XA` pseudo-locale must not break any
layout — that is a real test, not a formality.

### Design-lab first

`web/pages/designs/` is an existing design lab: concept studies rendered against fixtures in
`web/data/design-fixtures.ts`, with a `DesignSwitcher`. **The testimonial screens are built there
first**, against fixtures, before any API exists. That is how the UI gets reviewed and iterated
cheaply — and it is half the answer to "an environment where I can test the backoffice".

### Screens

**`/testimonials` — three tabs**

- **Suggested** — the ranked candidate inbox. Each row: the quote excerpt, star rating, score as a
  compact meter, the top three reasons as chips, source project, and two actions —
  *Request testimonial* (opens a dialog to compose the message and pick requested fields) and
  *Dismiss*. This is the screen that makes the product feel smart, so it gets the most design care.
- **Library** — `.data-table` of testimonials: quote, author, status pill, consent pill, tags,
  walls it appears in. Filters by status, consent state, tag, project, media type.
- **Walls** — cards with a live thumbnail, item count, status, and the embed snippet.

**`/testimonials/[id]` — detail**

Original vs published quote shown as a diff, so an edit is always visible. The consent record is
rendered as a plain-language panel: what was granted, when, under which notice version, and a
prominent revoke control. Media with transcode status. Tags. Wall membership.

**`/testimonials/walls/[id]` — wall editor**

Two panes: the library on the left with suggestions surfaced at the top, the wall on the right as a
drag-to-order list. Layout and theme controls. A live preview `<iframe>` pointed at the mock site so
the customer sees the *real* embed, not an approximation. Publish is an explicit, separate action
with a confirmation showing exactly what will become public.

### Permissions and gating

New tenant permissions: `testimonial:read`, `testimonial:create`, `testimonial:update`,
`testimonial:delete`, and — deliberately separate — **`testimonial:publish`**, because pushing
content onto the public internet is a higher-trust action than editing a draft. Added to
`AllTenantPermissions`, the role defaults, and `web/data/permissions.ts`.

Plan gate: a new `config.FeatureTestimonials`. Native video is *additionally* gated by the existing
`config.FeatureVideo`, so the video pipeline cannot be reached on a plan that has no video.

---

## Security, privacy and compliance

This feature publishes named individuals' words and faces on the public internet. It gets treated
accordingly.

- **RLS everywhere**, including on every public read path, via the resolve-then-`set_config`
  pattern already used by `widgetanalytics/public_http.go`.
- **No raw IP addresses** in consent evidence — a UA family, a coarse region and a hash of the
  notice snapshot are sufficient proof and far less sensitive.
- **Revocation is immediate and self-serve.** Every consenting author gets a permanent revoke link.
  Revoking rebuilds affected snapshots inside the same transaction and purges media; worst-case
  public exposure is bounded by the 60 s cache ceiling.
- **Erasure cascades.** `backend/internal/privacy` must treat testimonials as first-class subject
  data: an erasure request for a feedback author revokes and purges their testimonial and its
  media. This is a required slice, not a follow-up. `docs/privacy-data-map.md` gets updated in the
  same change.
- **Tombstones.** A revoked testimonial keeps only `id`, `revoked_at` and a hash of the consent
  evidence — enough to defend the record that consent existed and was withdrawn, with none of the
  personal data retained.
- **Audit.** `publish`, `unpublish`, `approve` and `revoke` land in `tenant_access_events`, visible
  in Settings → Audit, following the `issuetracker` precedent.
- **Abuse.** Public collection endpoints are rate-limited, honeypotted, and optionally
  Turnstile-gated; the public form has a per-day submission cap.

## Observability

- `testimonial_candidates_scored_total{eligibility}`
- The collection funnel — `requests_sent → opened → submitted` — which is the single most
  important product metric here, since a feature nobody's customers respond to is dead
- `testimonial_snapshot_build_seconds`, `testimonial_snapshot_rebuild_total{trigger}`
- `testimonial_embed_requests_total{cache}`, so we can see the cache actually working
- `testimonial_transcode_seconds`, `testimonial_transcode_failures_total{kind}`
- Structured logs on every consent state transition, with the testimonial ID and no PII

---

## Test environment (the two surfaces)

### 1. Backoffice

- Design-lab study at `/designs/testimonials/*` against fixtures — reviewable with no backend.
- `admin seed testimonials` CLI subcommand (extending `backend/cmd/fidibeki-api`) seeding a demo
  tenant with **~30 feedbacks of deliberately varying quality** — glowing-and-specific,
  glowing-but-empty ("nice!"), negative, PII-contaminated, profane, too-short — so the scoring is
  visibly doing something rather than ranking noise. Plus consented testimonials with a photo and a
  short sample video, and two published walls.

### 2. Mock customer website

A new `mocksite` Compose service (Caddy `file_server`) on **`demo.localhost:8088`** — a genuinely
different origin from the app, so CORS, CORP and the referrer allowlist are actually exercised
rather than accidentally bypassed by same-origin requests.

Three pages, each proving something different:

- `/` — a landing page with a grid wall via the `<script>` + `<fidibeki-wall>` route
- `/pricing` — a carousel wall with a video testimonial, proving playback and captions
- `/headless` — server-rendered from the public JSON API, proving the SSR/SEO path

A Caddyfile matcher routes `demo.localhost` to the new service; the compose service mounts the
static site and the built widget bundle.

**Local-run caution:** worktrees share one Compose project and one migration number space. Run with
an isolated project name (`docker compose -p fidibeki-testimonials …`) and confirm migration
numbering against `origin/main` before starting.

---

## Delivery — TDD, vertical slices

Every slice is a tracer bullet: schema → store → service → HTTP → UI, with the test written first.
Go unit tests for pure logic, testcontainer integration tests for anything touching Postgres
(matching the existing `*_integration_test.go` convention), vitest for the widget and Vue
components, Playwright for end-to-end.

Note the local lint gap: `make lint` only runs `go vet`, while CI runs `golangci-lint`. Run
`golangci-lint` locally before pushing each slice.

| # | Slice | Tests written first |
| --- | --- | --- |
| 1 | Scoring engine (pure) | Table-driven: every hard gate, every signal, boundary lengths, score monotonicity, `reasons` ordering, version pinning |
| 2 | Candidates: schema, store, scoring job | Integration: upsert idempotency, RLS isolation between tenants, decay ordering, dismissal, lazy rescore on version bump |
| 3 | Testimonial aggregate + consent state machine | Unit: every legal and illegal transition; the substring edit rule (normalisation, trimming, paraphrase rejection); the publishable predicate |
| 4 | Requests, tokens, public collection endpoint | Integration: token hashing, constant-time lookup, expiry, single-use submission, rate limit, honeypot, RBAC on the customer side |
| 5 | Media upload + verification | Integration: presign/complete flow, structural validation rejects a renamed file, safety-status gating, oversize and over-duration rejection |
| 6 | Transcode worker + renditions | Contract test against a fake transcoder; one real ffmpeg test asserting faststart and poster generation; retry idempotency by `(media_id, kind)` |
| 7 | Walls, snapshots, public JSON endpoint | Integration: explicit ordering, consent-scope projection, ETag stability and change, revocation forces an immediate rebuild, 304 handling, RLS on the public path |
| 8 | Web Component | vitest + happy-dom: renders from a payload, Shadow DOM isolation, `::part` hooks, reduced-motion, no-autoplay, graceful failure. Playwright against the mock site for the real cross-origin path |
| 9 | Backoffice | Design-lab study → component tests → Playwright: suggest → request → collect → approve → add to wall → publish → appears on the mock site |
| 10 | Privacy cascade + audit | Integration: erasure revokes and purges; tombstone retained; audit events recorded; `docs/privacy-data-map.md` updated |
| 11 | Standalone collection forms | Integration: public form submission, per-day cap, disabled form returns 404. *Cuttable if scope needs trimming* |

Slices 1–3 have no UI and no external dependency, so they can land and be reviewed quickly. Slice 7
is the architectural keystone — once snapshots exist, both embed paths and the whole backoffice
become straightforward.

## Known risks

- **Video is the scope risk.** ffmpeg, a new worker container, captions, range requests and a
  player are collectively larger than everything else in v1 combined. If the schedule slips, slices
  5–6 are the natural place to cut back to photo-only, and nothing else in the design depends on
  them.
- **Consent UX friction.** Mandatory opt-in means some percentage of requests are never answered.
  The funnel metrics exist precisely so we can see this and tune the email and form, rather than
  guessing.
- **Scoring credibility.** If the "Suggested" tab surfaces mediocre quotes in week one, customers
  stop trusting it permanently. The seed data with deliberately varied quality is there to catch
  this before a customer does.
