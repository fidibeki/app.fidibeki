# Audit and correction plan — testimonials (issue #107)

> Audit of the design in [`issue-107-testimonials.md`](issue-107-testimonials.md) and of the slice 1
> implementation in `backend/internal/testimonial/`.
> Three implementation defects were found and **fixed in this branch**. Nine design defects were
> found; each has a correction below, ordered by severity, to be applied before the affected slice
> is built.

Two of the findings contradict claims made earlier in this issue. Those are called out explicitly
rather than quietly corrected, because the claims were the reason the work looked finished.

---

## Implementation defects — found and fixed

### A1 · Critical · The PII gate was inoperative for exactly the tenants it protects

**What was wrong.** `Score` gated on `ModerationFlags` containing `pii`, `hate`, `harassment`,
`sexual`, `violence` or `self_harm`. But an empty flag slice means one of two very different things:
the moderation pass ran and found nothing, or it never ran. The engine treated them identically.

It never ran is the common case. Migration `000025` sets
`text_process_status = 'skipped'` for every **Starter-plan** tenant, and `config/plans.go` gives
Starter no `AI` feature — so `text_moderation_flags` for those tenants is permanently `'{}'`. Every
Starter feedback would have passed the PII gate unconditionally, and a glowing quote containing a
customer's email address or order number would have been suggested for **publication on the open
internet**.

Worse, the test suite asserted this as correct behaviour.
`TestScoreDegradesGracefullyWithoutAISignals` set the AI fields to nil and required `Eligible`. The
plan's phrasing — *"the AI is an accelerant, never a dependency"* — was true of ranking and false of
safety, and the code encoded the wrong one.

**Fix (applied).** `Input.ModerationStatus` is required and mirrors
`feedbacks.text_process_status`. Anything other than `completed` yields `Ineligible` with
`ReasonAwaitingModeration`. The test that asserted the old behaviour was replaced by
`TestScoreRefusesToRankUnmoderatedText`, which covers `pending`, `processing`, `failed`, `skipped`
and the empty string.

Ranking still degrades gracefully without optional AI signals — a nil `SentimentScore` still scores
as neutral. Safety no longer degrades at all. They are different properties.

**Consequence requiring a product decision — see D9.**

### A2 · Major · Specificity scoring was biased in favour of English

**What was wrong.** `comparisonPattern` counted bare `to`, `from`, `more`, `less`, `mais` and
`menos` as evidence of a measurable claim. Those words appear in most ordinary English sentences and
in materially fewer Portuguese ones. Measured:

| Text | Specificity |
| --- | --- |
| *"We really like this product and the team behind it is genuinely lovely."* | 0.000 |
| *"We really like this product and it is lovely **to** deal with the team."* | 0.286 |
| *"Gostamos mesmo deste produto e a equipa por trás dele é muito simpática."* | 0.000 |

Identical vague praise scored 0.286 in English and 0.000 in Portuguese, purely for containing the
word "to". Since specificity carries weight 0.20, English fluff gained ~0.057 of base score over
Portuguese fluff for no reason.

**This contradicts what I claimed.** The commit message for `379e807` and the issue comment both
stated that bilingual detection was in place *specifically* so that Portuguese testimonials would
not rank below English ones. The patterns were bilingual; the *outcome* was not, and I did not
measure it before claiming it.

**Fix (applied).** `comparisonPattern` now matches only verbs and comparatives that carry a
measurable claim on their own (`faster`, `halved`, `reduced`, `saved` / `reduzimos`, `poupámos`,
`cortámos`, `metade`). A new structural `rangePattern` detects *"from 3 days to 20 minutes"* and
*"de 3 dias para 20 minutos"* — a before-and-after number pair, which is the strongest concrete
claim a testimonial makes and behaves identically in both languages because it is structural rather
than lexical.

Guarded by two new tests: `TestScoreGivesVaguePraiseNoSpecificityInEitherLanguage` (seven vague
samples across both languages, all must score exactly 0) and
`TestScoreSpecificityIsLanguageNeutral` (equivalent concrete quotes must score *identically*).

### A3 · Minor · Dead speculative API

`TextRunes` was exported "for callers persisting candidate signals". There were no such callers, it
had 0% coverage, and it duplicated the trimming logic used elsewhere with a different function.
Removed.

**Post-fix state:** 97.8% statement coverage, `gofmt`, `go vet` and `golangci-lint` all clean.

---

## Design defects — corrections required before the affected slice

### D1 · Critical · Collection requests would violate purpose limitation

**The defect.** The plan sends the collection request to `feedbacks.reporter_email`. That address is
collected under a consent whose purpose this codebase pins to a single value:

```go
// ReporterConsentPurposeFeedbackUpdates is the only supported consent purpose for reporter emails.
// Any other purpose value is an unknown/contradictory payload and is rejected.
const ReporterConsentPurposeFeedbackUpdates = "feedback_updates"
```

Soliciting a testimonial is not an update about their feedback. It is a different purpose, and this
repository has deliberately built machinery — a validated purpose field, a versioned notice
snapshot, a controller profile — to make that distinction enforceable. The plan walked straight
through it.

It is compounded by the scoring engine: `AuthorReachable` contributes 0.10 to `base_score`, so the
design *ranks candidates higher* on the strength of a permission it is not entitled to use.

**Correction.**

1. Add a distinct consent purpose, `testimonial_requests`, alongside `feedback_updates`, with its
   own notice text stating plainly that the customer may ask to publish their words.
2. The widget's consent step offers it as a **separate, independently declinable** opt-in — never
   bundled, never pre-ticked.
3. `AuthorReachable` in scoring means "holds a live `testimonial_requests` consent", not "has an
   email address". Rename the signal to `ContactPermitted` so the code cannot drift back.
4. For the existing corpus collected before this purpose existed, requests are **not** sendable.
   Instead, surface a "share a link" flow: the customer copies a collection link and delivers it
   through a channel where they already have a relationship. This keeps the feature useful on day
   one without retroactively repurposing consent.
5. Record the decision in `docs/privacy-data-map.md` and the ROPA registry, which `make
   governance-check` already enforces.

### D2 · Critical · Public endpoints ignore tenant lifecycle

**The defect.** `widgetapi.Authorizer.Authorize` refuses to serve widget configuration unless the
tenant is `StatusActive`, or on a trial that has not expired, and unless the origin is allowed. The
plan's public wall, media and beacon endpoints have no equivalent check.

A suspended tenant, a deleted tenant, or one whose trial lapsed would keep serving testimonials from
our domain indefinitely — a billing hole and, for a tenant suspended for abuse, a moderation hole.

**Correction.** Introduce `testimonial.PublicAuthorizer`, mirroring `widgetapi.Authorizer`: resolve
the tenant from the wall public ID, then assert lifecycle status, trial validity and the
`FeatureTestimonials` entitlement before any snapshot is served. Return 404 rather than 403 — the
existence of a wall belonging to a suspended tenant is not information a caller is owed. A suspended
tenant's snapshot rows are retained, not deleted, so reactivation restores the wall intact.

### D3 · Major · The stale-snapshot fallback is unbounded

**The defect.** The revocation design says a stale snapshot is never served, "falling back to a
live-projected response for that request". That fallback is precisely the multi-table join under RLS
that snapshots exist to avoid — and it triggers on *every* request to a popular wall until the
rebuild lands. A single revocation becomes a thundering herd against the origin.

**Correction.** Do not project live. Serve the existing snapshot **filtered through a cheap indexed
lookup** of revoked testimonial IDs for that wall (`WHERE consent_state <> 'granted'`, an indexed
scan over a handful of rows), and mark the response `Cache-Control: no-store` while stale. Correct,
bounded, and roughly the cost of the normal path. The full rebuild still runs on the queue.

### D4 · Major · No rate limiting on the public surface

The snapshot, media and impression-beacon endpoints are unauthenticated and unrated in the plan.
Apply the existing `ratelimit` package per wall public ID and per source address, and cap beacon
writes per wall per minute. Without this, the beacon is an amplification primitive and the media
redirect is a free presigned-URL generator.

### D5 · Major · Collection-form public IDs left at 8 characters

Wall and media public IDs were raised to 16 characters (~95 bits) after the external review flagged
enumeration across tenants. `testimonial_forms.public_id` (`tf_`) was left at 8 (~47 bits) in the
same document. A public collection form is exactly as enumerable as a wall, and enumerating one lets
an attacker submit forged testimonials to a tenant they have no relationship with. Raise to 16.

### D6 · Major · No cleanup semantics for wall membership

`testimonial_wall_items` rows survive their testimonial being archived or revoked. The plan says
snapshots are rebuilt, which handles what is *displayed*, but leaves the wall editor showing ghost
entries and leaves `position` numbering with holes that the deferrable unique constraint will not
repair. Specify: archiving or revoking removes the item and renumbers remaining positions in the
same transaction, and the wall editor surfaces the removal so the customer is not silently short one
card.

### D7 · Minor · The i18n keys do not exist

The scoring engine returns `testimonials.reason.*` and `testimonials.ineligible.*` keys. None exist
in `web/i18n/locales/{en,pt,pt-PT,pt-AO,en-XA}.ts`, so the Suggested tab would render raw keys.
Add them with the backoffice slice, and add `en-XA` pseudo-locale coverage so a long translation
cannot break the candidate row layout.

### D8 · Minor · The revocation link is a permanent unauthenticated capability

The revoke token never expires by design, which is right — a revocation route that expires is worse
than one that does not. But a forwarded email lets a third party revoke someone else's testimonial.
Add a confirmation step on the revoke page and email the author a notification when a revocation is
completed, so an unwanted revocation is at least visible. Do not add expiry.

### D9 · Minor, but needs a product decision · Starter tenants now get no candidates

A consequence of the A1 fix: Starter has no AI, so `text_process_status` is permanently `skipped`,
so no Starter feedback is ever eligible. This is the correct *safe* default and is what ships today.

Three ways forward, in order of preference:

1. **`FeatureTestimonials` requires AI** — testimonials become a Growth-and-up feature. Simplest,
   honest, and defensible: we cannot safely publish text we have not screened.
2. **A deterministic PII prefilter for Starter** — regex screening for emails, phone numbers, IBANs
   and card-shaped digit runs, enough to clear the gate without a model. More work, weaker
   guarantee, but keeps the feature on the entry plan.
3. Offer AI text processing to Starter for testimonial candidates only, at our cost.

Recommendation: **(1)**, with **(2)** as a follow-up if Starter conversion data justifies it.

---

## Ordering

D1 and D2 are prerequisites for slices 4 and 8 respectively and must land before them. D3–D6 are
corrections to slices already specified and should be folded into those slices rather than deferred.
D7 lands with the backoffice slice. D8 lands with the consent slice. D9 needs an answer before the
plan gate is written.

Nothing here changes the migration block `000116`–`000120` except D5 (a column width) and D6 (the
cleanup routine), both of which are still unwritten — so no migration has to be revised after the
fact.
