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

```
status        = 'approved'
consent_state = 'granted'
consent_scopes->>'quote' = 'true'          -- a testimonial without a consented quote is not a testimonial
every referenced media is safety-clean, transcoded, and within a granted scope
video media (if any) has passed publish-time human review
```

The `quote` scope is a hard requirement, not an optional projection. Without it a card could render
a name, a company and a *video of someone speaking words they did not consent to publish* — which
re-opens the edit-rule problem through the transcript. Video-without-quote is not a publishable
shape.

This predicate lives in one function, is unit-tested exhaustively, and is the only thing the
snapshot builder consults.

### The edit rule

The author consents to *specific words*. If the customer then changes the quote, the consent no
longer covers what is published.

An earlier draft of this plan allowed free shortening — "the published quote must be a contiguous
substring of the consented text". **That rule is wrong and has been removed.** Truncation is not
meaning-preserving:

> consented: *"I did not think this would work, but it does"*
> published: *"I did not think this would work"*

Both are contiguous substrings. The second inverts the author's meaning and would have required no
re-consent. The same hole swallows every contrastive pattern — *"It's not cheap, but it's worth
it"*, *"Support was slow until I escalated, then great"*. A lexer that detects contrastive
conjunctions was considered and rejected: this platform serves `en`, `pt`, `pt-PT` and `pt-AO`, so
it would need to catch *but / however / though* **and** *mas / porém / contudo / no entanto / apesar
de*, in a domain where a false negative publishes a defamatory-by-omission quote. That is not a
lexer anyone should trust.

The actual rule:

> The published `quote` must equal `consented_text` exactly, modulo Unicode NFC normalisation and
> leading/trailing whitespace. **Any** other change — including shortening — flips `consent_state`
> to `pending_reconsent`.

To keep this usable rather than merely safe, **re-consent is one click**. The customer proposes an
edit; the author receives an email showing the original and the proposed version side by side with
an Approve and a Reject button, each a signed single-use link. No form, no login. The customer gets
their trim, and the author's words stay the author's words.

The rule stays a pure function and is still exhaustively unit-testable — but now the *policy* it
encodes is defensible, which the previous one was not.

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
- `base_score numeric(6,5) CHECK (base_score BETWEEN 0 AND 1)` — the undecayed score.
  `numeric(6,5)` and not `numeric(5,5)`: the latter caps below `1.0` exclusive and could not store a
  perfect score. The CHECK, not the precision, is what constrains the range.
- `decayed_score numeric(6,5)` — `base_score * exp(-age_days / 90.0)`, **materialised**, with
  index `(tenant_id, eligibility, decayed_score DESC) WHERE dismissed_at IS NULL`
- `score_version smallint` — bump to force a rescore, processed by a job, never on the read path
- `signals jsonb` — every input with its contribution, for explainability and debugging
- `reasons text[]` — i18n keys, e.g. `testimonials.reason.concreteOutcome`
- `eligibility text` — `eligible | ineligible`
- `ineligible_reasons text[]` — hard-gate failures
- `suggested_excerpt text`, `suggested_headline text`, `suggested_tag_ids uuid[]` — AI enrichment,
  nullable; the row is fully useful without them
- `dismissed_at`, `dismissed_by` — "never suggest this again"
- `computed_at`
- Indexes: `(tenant_id, eligibility, decayed_score DESC) WHERE dismissed_at IS NULL`,
  `(tenant_id, project_id)`

**Why decay is materialised rather than computed at read time.** An earlier draft applied
`exp(-age_days / 90.0)` in the query to avoid a nightly rescan. That was a bad trade: an index
ordered on `base_score` cannot satisfy an ORDER BY on the decayed value, so every load of the
Suggested tab would scan all eligible candidates for the tenant and sort them in memory. Trading a
bounded nightly job for an unbounded per-read sort is the wrong direction. The nightly job only
touches `eligible`, non-dismissed rows — a small fraction of all feedback — and runs off the hot
path.

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
- `UNIQUE (tenant_id, request_id) WHERE request_id IS NOT NULL` — a request can produce at most one
  testimonial. Checking `testimonial_requests.status` alone does not survive concurrency: two
  simultaneous submissions both read `pending` and both insert. The submission transaction also
  takes `SELECT … FOR UPDATE` on the request row, but the constraint is what makes the invariant
  true rather than merely likely.
- `review_note text NULL` — an internal note for the approver ("legal wants the company name
  removed"). Never rendered publicly.

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

`testimonial_media` carries **two** partial unique constraints, because the asset exists before the
testimonial does:

- `UNIQUE (tenant_id, testimonial_id, role) WHERE testimonial_id IS NOT NULL` — after finalisation
- `UNIQUE (tenant_id, request_id, role) WHERE request_id IS NOT NULL AND testimonial_id IS NULL` —
  during collection

The second is not redundant. Media uploaded through the collection form has `testimonial_id IS
NULL`, so the first constraint does not fire, and an author who uploads two avatars would produce
two rows with no defined winner. `complete_token_hash` deduplicates the *completion call*, not the
role-per-request invariant.

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
- `is_current boolean NOT NULL DEFAULT false` with
  `CREATE UNIQUE INDEX … ON testimonial_wall_snapshots (tenant_id, wall_id) WHERE is_current`
- `is_stale boolean NOT NULL DEFAULT false` — set synchronously when a consent revocation affects
  this wall. **A stale snapshot is never served** (see the revocation design below), so the flag is
  a correctness mechanism, not a hint.
- Retaining old versions is what makes "unpublish"/rollback trivial and gives us an audit trail of
  what was publicly visible when — which matters if consent is ever disputed.

`testimonial_wall_daily_views` — `(tenant_id, wall_id, day)` with `impressions bigint`. A single
counter, incremented from the embed's fire-and-forget beacon. Deliberately minimal, surfaced in the
UI as **"Estimated views"**, and never used for billing or quota enforcement — the endpoint is
unauthenticated and therefore trivially inflatable.

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
- **`rating IS NOT NULL AND rating <= 2`** — the author told us they are unhappy
- a testimonial already exists for this feedback, or the candidate was dismissed
- detected language is not among the tenant's supported locales

Two gates carry most of the weight.

**The PII gate**: a feedback containing an email address or an order number must never be
*suggested* for publication, no matter how glowing.

**The low-rating gate**: without it, a 1-star review whose AI `sentiment_score` reads high — pure
sarcasm, or a model scoring *"great, waited four hours for support"* on tone — could rank as a top
candidate purely on length and specificity, because `rating 4–5` only *adds* 0.25 and low ratings
were penalised by nothing. Surfacing an angry customer as a suggested testimonial is precisely the
week-one failure that destroys trust in this feature permanently. `rating = 3` gets a −0.10 penalty
rather than a gate; ambivalence is not hostility.

### Weighted signals

| Signal | Rationale | Weight |
| --- | --- | --- |
| `rating` 4–5 | Explicit satisfaction | 0.25 |
| `sentiment_component` (see below) | Tone independent of the star | 0.20 |
| Length in the 120–600 char band | "nice" is worthless; an essay does not fit a card | 0.15 |
| Specificity — numbers, percentages, time units, comparatives | "cut onboarding from 3 days to 20 minutes" is the whole game | 0.20 |
| `actionability = 'not_actionable'` | Actionable feedback is a bug report, not praise | 0.08 |
| Author reachable (`reporter_email` present + `notify_reporter`) | A candidate we cannot ask is worth less in practice | 0.10 |
| `rating = 3` | Ambivalence, not hostility | −0.10 |
| `profanity` flag | Penalty, not a gate | −0.12 |

**The sentiment transform, stated explicitly:** `ai.Result.SentimentScore` is validated to
**`[-1, 1]`** (`backend/internal/ai/types.go:95`), not `[0, 1]`. Feeding it in raw would let a
negative-sentiment text subtract from the score by accident rather than by design. The signal is

```
sentiment_component = (SentimentScore + 1) / 2      // → [0, 1]
```

This gets a dedicated unit test with a high-rating / negative-sentiment input, because that
combination is the sarcasm case and it must not score well.

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

### The re-consent page

The edit rule sends the author back for approval, so that path needs a real design rather than an
implied one. `/testimonials/reconsent/{token}` is a single screen: the original text, the proposed
text with the difference highlighted, and two buttons — **Approve this wording** and **No, keep my
original**. Rejecting reverts `quote` to `consented_text` and returns `consent_state` to `granted`,
so a rejected edit is not a dead end for the customer.

Its token reuses the `testimonial_requests` machinery with `kind = 'reconsent'` — same hashing, same
expiry semantics, same rate limiting — rather than a second parallel token system.

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

   Three flags that are not optional in practice, because phones are the capture device:
   **rotation** (portrait recordings carry a display matrix; without handling it every phone video
   renders sideways), **audio loudness normalisation** (`-af loudnorm=I=-16:TP=-1.5:LRA=11`, so a
   wall of testimonials does not swing 20 dB between cards), and **BT.709 tone mapping** for HDR
   sources (`-colorspace bt709 -color_primaries bt709 -color_trc bt709`, otherwise iPhone HDR
   footage renders washed out).

   Adaptive bitrate (HLS/fMP4 ladders) is **deliberately out of scope**: at a 90-second cap and a
   single 720p rendition, progressive download is adequate, and an ABR ladder would multiply
   transcode cost and storage for a format most testimonial cards never play to completion.
   Revisit if analytics show buffering.

   Idempotent by `(media_id, kind)`; retries are safe. Bounded attempts, then `failed` with an
   operator-visible error.
4. **Captions** — drafted with Gemini, stored as `captions_vtt`, and **editable by the customer
   before publish**. The embed ships a `<track>` only when captions are `ready`. Auto-captions that
   nobody reviewed are an accessibility liability, so they are a draft, not a fact.

   To be precise about the cost: this is the same *model* as the existing audio transcription
   (`ai.Result.Transcription`), but it is **new wiring, a new ai-pool work type and a new cost
   line** — the existing field is produced by the feedback pipeline for feedback audio, not for
   testimonial video. Calling it free reuse would be wrong.
5. **Publish-time review gate** — see below.
6. **Deliver** — see above.

### The publish gate: we become a publisher of user video

Upload-time safety scanning is necessary and not sufficient. Between upload and publication the
plan previously had *nothing*: no re-scan, no human review, no kill-switch. Yet publication is the
moment Fidibeki starts serving someone's video from a Fidibeki domain to the open internet.

Three failure modes that an upload-time scanner cannot close:

- The scanner simply misses a class of content.
- The content is legal but defamatory, abusive, or otherwise something no automated classifier can
  judge.
- **The video contains an identifiable person who is not the author.** This one is not a scanner
  problem at all. The author's consent covers the author's words and the author's likeness. It does
  not cover their colleague in the background, or a customer's face on a screen behind them.

Therefore:

- **A wall containing a video cannot transition to `published` until that video has passed explicit
  human review** by a member holding `testimonial:publish`. The reviewer watches it; the system
  records who approved what and when.
- **The collection form carries a depicted-person warranty.** Before recording, the author confirms
  that they have permission from anyone else identifiable in the video. This does not transfer
  liability away from us, but it makes the obligation explicit and gives the review gate something
  to check against.
- **A platform-level media kill-switch.** A platform admin can purge a `content_hash` across every
  tenant and wall in one action: mark the media rejected, force affected snapshots stale, delete the
  objects, and issue a CDN tag purge. A 404 at origin is not sufficient once bytes are at the edge —
  which is why tag-based purge is a stated CDN requirement for video.

This is why the media cache policy is 300 seconds and not a year. The two decisions are the same
decision.

### Public media delivery — two traps, not one

**Trap 1: never put a presigned URL inside a cacheable snapshot.** A presigned URL expires; the
snapshot does not. Two hours later the wall shows broken images and nobody knows why.

**Trap 2: do not solve trap 1 by proxying every byte through the API.** A 50 MB video viewed 10,000
times is 500 GB of API egress, plus API CPU, plus a Postgres round trip per request — and correct
`Range`→S3-`Range`→206 proxying is fiddly to get right. An earlier draft of this plan implied
exactly that and was silent on the cost.

The design that avoids both:

```
GET /v1/public/media/{mediaPublicID}/{contentHash}.{ext}
    → 302 to a short-TTL (60 s) presigned S3/MinIO URL
```

The **snapshot embeds the stable path**, never the presigned URL. The API resolves the tenant from
the media public ID, reads under RLS (`set_config('app.tenant_id', …, true)`), checks the live
consent gate, and issues a redirect. S3/MinIO — or a CDN in front of it — absorbs the bandwidth and
handles range requests natively. The API handles one cheap request per *view*, not per *byte*.

Headers on the redirect itself: `Cache-Control: public, max-age=300, must-revalidate`,
`Cross-Origin-Resource-Policy: cross-origin`, `X-Content-Type-Options: nosniff`.

**Deliberately not `immutable`, and deliberately not a one-year `max-age`.** Content addressing
makes those *cache-correct*, and an earlier draft used them for exactly that reason — but they are
*consent-incorrect*. `immutable` tells a browser not to send even a conditional request, so a
revoked author's face and voice could keep playing from a visitor's disk cache for up to a year
with no mechanism to stop it. 300 seconds is the deliberate trade: slightly more revalidation
traffic in exchange for a revocation window measured in minutes.

A CDN in front of the media origin is **recommended but not required** for photo-only operation,
and **required before enabling video on a production tenant**. The CDN must support tag-based purge,
which is what makes the platform kill-switch (below) actually work.

---

## Public embed

### Endpoints (on the existing widget host)

```
GET /v1/public/walls/{wallPublicID}          → snapshot JSON, strong ETag
GET /v1/public/media/{mediaPublicID}/{hash}  → immutable media bytes
POST /v1/public/walls/{wallPublicID}/views   → fire-and-forget impression beacon
```

Cache policy on the snapshot: `public, max-age=60, stale-while-revalidate=300`.

**An honest statement of the revocation window.** An earlier draft claimed this "bounds worst-case
exposure after a consent revocation to one minute". That was false, and the correction matters
because it is what we tell authors:

| Layer | Worst case after revocation |
| --- | --- |
| Origin (live consent gate) | Immediate — the next request is already blocked |
| CDN edge, snapshot JSON | `max-age` + `stale-while-revalidate` = **360 s** |
| Visitor browser, snapshot JSON | up to 60 s, served from disk with no revalidation |
| Visitor browser, media bytes | up to 300 s (see the media cache decision above) |

So: **revocation blocks new viewers immediately at the origin, and viewers who already loaded the
page may retain it for up to about six minutes.** That sentence goes in the consent notice the
author signs, verbatim. Promising instantaneous erasure from other people's browsers would be a
lie, and a discoverable one.

The impression beacon is an unauthenticated public endpoint, so its numbers are **approximate by
construction** — anyone can inflate them. It is rate-limited per wall and per IP, and the UI labels
it as an estimate. Presenting it as an exact analytics figure would be misleading.

### `allowed_origins`: enforced, and labelled for what it is

Storing a documented non-control is the worst of both worlds — a customer sets it and believes it
protects them, and a future engineer wires it into something that *looks* like a security check.

So it is **enforced** on the snapshot endpoint as a `Referer`/`Origin` allowlist, and it is
**labelled in the UI as "casual reuse prevention", never as access control**, with one sentence
next to the field: *anyone who knows the wall ID can fetch this data directly; this setting only
stops your wall being embedded on another site by accident.* That is the truth, and the customer is
entitled to it before they decide what to publish.

Wall and media public IDs use **16 characters** (`^wl_[A-Za-z0-9]{16}$`, ~95 bits) rather than the
8 used for internal identifiers like `fb_`. Eight characters is ~47 bits — fine against a targeted
guess, thin against someone enumerating public wall IDs across every tenant on the platform to
harvest customer testimonials wholesale.

### Snapshot building

Publishing a wall walks `testimonial_wall_items` in `position` order, filters through the
publishable predicate, projects each testimonial through its `consent_scopes`, resolves media to
immutable URLs, and writes one `testimonial_wall_snapshots` row with a strong ETag
(SHA-256 of the payload).

Rebuild triggers:

- customer publishes / reorders / changes theme → rebuild the affected wall
- testimonial approved, archived, edited, or re-tagged → rebuild every wall containing it
- media transcode completes → rebuild
- consent revoked → see below

### Revocation: a live gate, not a synchronous rebuild

An earlier draft rebuilt every affected snapshot **synchronously inside the revoking transaction**,
on the theory that revocation "is not allowed to sit in a queue". That was wrong twice over.

It was a self-inflicted DoS: rebuilding walks `testimonial_wall_items`, projects consent scopes,
resolves media and hashes the payload. Under PgBouncer `pool_mode = transaction` with
`default_pool_size = 20` (`infra/pgbouncer/pgbouncer.ini`), a handful of concurrent revocations on
large walls can hold server connections for hundreds of milliseconds each and starve the tenant's
entire API.

And it was not even immediate: readers consult the *snapshot*, so between the `consent_state`
write and the rebuild commit, concurrent readers still see the old payload.

The correct decomposition — three cheap synchronous writes, one queued rebuild:

1. **Synchronously, in the revoking transaction:** flip `consent_state := 'revoked'`, stamp
   `revoked_at`, and set `is_stale := true` on the current snapshot of every affected wall. All
   three are single-row indexed updates holding no heavy locks.
2. **On the read path:** the public endpoint refuses to serve a snapshot with `is_stale = true`,
   falling back to a live-projected response for that request. This is what makes revocation
   genuinely immediate at the origin — the gate blocks reads *before* any rebuild finishes.
3. **Asynchronously, on a high-priority queue:** rebuild the snapshot and clear `is_stale`.
4. **Then:** purge the media objects.

The live consent gate is the load-bearing part. The queue is an optimisation.

### The Web Component

New esbuild entry points in `widget/`, reusing the existing hashed-manifest machinery:

- `widget/src/testimonials/loader.ts` → `dist/v1/testimonials.js` (IIFE, registers the element)
- `widget/src/testimonials/wall.ts` → the ESM implementation, code-split

Customer integration:

```html
<script async src="https://widget.fidibeki.com/v1/testimonials.js"></script>

<!-- a full wall -->
<fidibeki-wall wall="wl_ab12cd34ef56gh78"></fidibeki-wall>

<!-- a single quote, e.g. in a homepage hero -->
<fidibeki-testimonial testimonial="ts_9a8b7c6d5e4f3g2h"></fidibeki-testimonial>
```

The **single-testimonial element is not an afterthought** — for most customers the first integration
is one strong quote under the hero, not a wall of thirty. It ships in v1, reads a one-item snapshot
through the same endpoint shape, and costs almost nothing once the wall element exists.

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

`Review` JSON-LD is available but **off by default**, with a documented caveat that Google restricts
rich results for self-serving reviews on a company's own site. A customer can turn it on and accept
the caveat; we do not ship structured data by default that may read as an attempt to game review
snippets.

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
- **Requests** — the outstanding collection requests: who was asked, when, opened or not, submitted
  or not, with a follow-up action. An aggregate funnel metric tells us the feature is working; this
  list is what lets a *customer* act on it, and without it "we sent 40 requests" is unactionable.
- **Walls** — cards with a live thumbnail, item count, status, and the embed snippet.

### Notifications

Reusing the existing `membernotify` preferences, three events matter enough to notify on:

- a testimonial was **submitted** (someone said yes — act while it is warm)
- a testimonial edit is **awaiting the author's re-consent**, and when they answer
- **consent was revoked** — the one nobody must miss. A revocation that goes unnoticed is how a
  customer's homepage ends up quietly missing a quote, or how a legal complaint arrives before we
  know there is a problem.

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

The boundary is only meaningful if it is stated exactly, so:

| Action | Permission |
| --- | --- |
| View the library, candidates, walls | `testimonial:read` |
| Send a collection request, dismiss a candidate | `testimonial:create` |
| Approve a testimonial, edit a quote, tag, add to a wall, reorder a wall | `testimonial:update` |
| **Publish or unpublish a wall snapshot** | `testimonial:publish` |
| **Approve a video for publication** | `testimonial:publish` |
| Archive, delete, revoke on the author's behalf | `testimonial:delete` |

Staging a wall is `update`; making it public is `publish`. A holder of `update` can prepare
everything and a holder of `publish` makes the one decision that has consequences outside the
building.

Plan gate: a new `config.FeatureTestimonials`, plus a distinct **`config.FeatureTestimonialVideo`**.
Reusing the existing `FeatureVideo` was considered and rejected: it currently gates *private*
feedback video captured through the widget, and publicly redistributing video from our own domain is
a different trust level that a customer should be able to hold without the other — and that we may
want to withhold independently.

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
- **Tombstones, specified at column level.** "Keeps a hash of the consent evidence" is too vague to
  implement — `consent_evidence` holds a UA family and a coarse region, which in combination is
  still personal data, and prose does not say whether the column survives. Precisely: a revoked
  testimonial **retains** `id`, `tenant_id`, `revoked_at`, `consent_notice_version` and
  `sha256(consent_evidence::text)`. It **nulls** `consented_text`, `quote`, `headline`,
  `author_name`, `author_role`, `author_company`, `author_company_url`, `author_locale`,
  `consent_evidence`, `review_note`, and deletes every `testimonial_media` row and object. The
  migration and the purge routine list these columns explicitly, and the Slice 10 test asserts the
  retained set is exactly those five fields — so adding a column later without considering erasure
  fails a test rather than leaking quietly.
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
| 1 | Scoring engine (pure) | Table-driven: every hard gate incl. the low-rating gate, the `(s+1)/2` sentiment transform, a high-rating/negative-sentiment sarcasm case, boundary lengths, score monotonicity, `reasons` ordering, version pinning |
| 2 | Candidates: schema, store, scoring job | Integration: upsert idempotency, RLS isolation between tenants, `decayed_score` ordering uses the index (assert the plan), dismissal, rescore-on-version-bump as a job and **not** on the read path |
| 3 | Testimonial aggregate + consent state machine | Unit: every legal and illegal transition; the exact-match edit rule (NFC normalisation, whitespace, and explicit cases proving a meaning-inverting truncation is rejected); the publishable predicate incl. the `quote` scope requirement |
| 4 | Requests, tokens, public collection endpoint | Integration: token hashing, constant-time lookup, expiry, `UNIQUE (tenant_id, request_id)` defeats a concurrent double-submit, rate limit, honeypot, RBAC |
| 5 | Re-consent flow | Integration: edit flips to `pending_reconsent` and unpublishes; approve restores `granted`; reject reverts the quote; token expiry |
| 6 | Media upload + verification | Integration: presign/complete flow, structural validation rejects a renamed file, safety-status gating, oversize and over-duration rejection, `UNIQUE (tenant_id, request_id, role)` blocks a second avatar pre-finalisation |
| 7 | Transcode worker + renditions | Contract test against a fake transcoder; real ffmpeg tests asserting faststart, poster generation, **rotation applied to a portrait source**, and loudnorm; retry idempotency by `(media_id, kind)` |
| 8 | Walls, snapshots, public JSON endpoint | Integration: explicit ordering, consent-scope projection, ETag stability and change, **a stale snapshot is never served**, revocation blocks the very next read before any rebuild runs, 304 handling, RLS on the public path |
| 9 | Publish gate + kill-switch | Integration: a wall with an unreviewed video cannot be published; `testimonial:publish` is required; the platform kill-switch purges a `content_hash` across tenants and marks snapshots stale |
| 10 | Web Component (wall + single) | vitest + happy-dom: renders from a payload, Shadow DOM isolation, `::part` hooks, reduced-motion, no-autoplay, graceful failure. Playwright against the mock site for the real cross-origin path |
| 11 | Backoffice | Design-lab study → component tests → Playwright: suggest → request → collect → approve → add to wall → publish → appears on the mock site |
| 12 | Privacy cascade + audit | Integration: erasure revokes and purges; the tombstone retains **exactly** the five specified fields and nothing else; audit events recorded; `docs/privacy-data-map.md` updated |
| 13 | Standalone collection forms | Integration: public form submission, per-day cap, disabled form returns 404. *Cuttable if scope needs trimming* |

Slices 1–3 have no UI and no external dependency, so they can land and be reviewed quickly. Slice 8
is the architectural keystone — once snapshots and the live consent gate exist, both embed paths and
the whole backoffice become straightforward.

The five migrations `000116`–`000120` are claimed in **one commit**, not drip-fed: the number space
is shared across worktrees and a partially-claimed block is how two branches collide.

## External review

This plan was reviewed adversarially by **glm-5.2 via opencode** after the first draft. The review
raised 7 Critical, 8 Major and 8 Minor findings. **All 7 Criticals and all 8 Majors were accepted
and are incorporated above**, along with 7 of the 8 Minors. The consequential changes:

- the substring edit rule was **replaced** with exact-match-or-re-consent (it permitted publishing
  *"I did not think this would work"* from *"…, but it does"*)
- the "60 s revocation window" claim was **false** and is now stated honestly as ~6 minutes
- synchronous snapshot rebuild inside the revoking transaction was **replaced** by a live consent
  gate — it was both a connection-pool DoS and not actually immediate
- media delivery moved from API byte-proxying to short-TTL presigned redirects, and dropped
  `immutable` / 1-year caching, which was consent-incorrect for revocable content
- a publish-time human review gate for video, a depicted-person warranty and a platform kill-switch
  were added — the plan had no gate between upload scanning and public distribution
- `sentiment_score` is `[-1,1]`; the transform is now explicit
- a low-rating hard gate was added
- `is_current`, `UNIQUE (tenant_id, request_id)`, and the pre-finalisation media constraint were
  genuine spec bugs
- read-time recency decay was replaced with a materialised `decayed_score`

Two recommendations were **not** taken:

**Rejected — `numeric(5,5)` for `base_score`.** The review called `numeric(6,5)` over-wide and
suggested `numeric(5,5)`. That is incorrect: in PostgreSQL `numeric(p,s)` admits values below
`10^(p−s)`, so `numeric(5,5)` caps below `1.0` and cannot store a perfect score. `numeric(6,5)` is
kept, with an explicit `CHECK (base_score BETWEEN 0 AND 1)` doing the constraining — which is where
range enforcement belongs anyway.

**Deferred to the product owner — "cut native video from v1".** The review argued, reasonably, that
video is where every Critical clusters and that cutting it removes roughly half the surface area.
That is a sound engineering judgement, but native video was an explicit product decision, so it is
not mine to reverse. The mitigations are in the plan instead: the publish gate, the kill-switch, the
bandwidth model, and the rotation/loudness handling. **Slices 6–9 remain the clean cut line** if the
schedule demands it — the data model already supports adding video later with no migration.

## Known risks

- **Video is the scope risk, and larger than the first draft admitted.** ffmpeg, a dedicated
  `media-worker`, captions, a publish review gate, a kill-switch and a CDN requirement are
  collectively larger than everything else in v1 combined. The earlier claim that "nothing else in
  the design depends on video" was wrong: the *data model* is video-optional, but the publish trust
  model and the bandwidth model exist **because** of public video. Slices 6–9 are the cut line.
- **Consent UX friction.** Mandatory opt-in means some percentage of requests are never answered.
  The funnel metrics exist precisely so we can see this and tune the email and form, rather than
  guessing.
- **Scoring credibility.** If the "Suggested" tab surfaces mediocre quotes in week one, customers
  stop trusting it permanently. The seed data with deliberately varied quality is there to catch
  this before a customer does.
