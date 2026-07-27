# Plan: Per-Seat Pricing

> Source design: `docs/superpowers/specs/2026-07-27-fidibeki-seat-pricing-design.md`
> GitHub issue: [#131](https://github.com/fidibeki/app.fidibeki/issues/131)

## Architectural decisions

- **Pricing contract**: Starter, Growth, and Scale publish their existing amounts as unit prices
  per seat; Enterprise remains custom.
- **Seat definition**: every non-removed workspace member consumes one seat. Deactivated members
  retain a seat, removed members free it, and pending invitations do not reserve one.
- **Legacy safety**: subscriptions that predate this migration remain `legacy_flat` and are never
  mutated by automatic seat synchronization.
- **Provider boundary**: new checkout quantity is the live billable-seat count. Stripe-confirmed
  quantity is stored separately and changes through idempotent subscription-item updates.
- **Convergence**: the worker periodically derives desired quantity from durable membership data
  and reconciles mismatches. Membership writes do not depend on Stripe availability.
- **Limits**: the current 1, 3, and 10 member caps remain. Invitation acceptance rechecks the cap
  under a tenant lock.
- **Compatibility**: API changes are additive; no existing billing fields, subscriptions,
  membership records, or Stripe identifiers are discarded.
- **Delivery target**: staging only. Production subscription conversion is out of scope.

---

## Phase 1: Honest Per-Seat Checkout

**User stories**: A buyer can understand the unit price and create a subscription whose initial
quantity matches the workspace members. An existing subscriber is not repriced by deployment.

### What to build

Introduce the billing model and confirmed-seat state, expose the price unit through the plan
catalog, initialize legacy and new tenants safely, and pass the live seat count to Stripe checkout.
Update pricing and signup surfaces so every self-service price is explicitly per seat.

### Acceptance criteria

- [ ] Existing subscribed tenants migrate to `legacy_flat` at quantity one.
- [ ] Existing trials and future signups use `per_seat`.
- [ ] The catalog exposes per-seat pricing metadata without removing existing fields.
- [ ] Checkout quantity equals the current non-removed member count with a minimum of one.
- [ ] Marketing, structured offers, and signup render a per-seat unit.
- [ ] Migration, catalog, checkout, parity, and responsive browser tests pass.

---

## Phase 2: Automatic Seat Convergence

**User stories**: A workspace owner can see what is billed, and member additions or removals
eventually update Stripe without maintaining a second quantity.

### What to build

Read subscription item identity and quantity from Stripe, persist provider-confirmed quantity from
webhooks, reconcile per-seat mismatches in the worker with prorations and idempotency, and expose
billable seats, confirmed seats, recurring estimate, and synchronization state in billing.

### Acceptance criteria

- [ ] Subscription retrieval rejects missing or ambiguous line items.
- [ ] Webhooks persist the Stripe-confirmed quantity idempotently.
- [ ] The worker converges add/remove mismatches and retries provider failures.
- [ ] Legacy subscriptions are excluded from automatic updates.
- [ ] A membership change during reconciliation remains detectable on the next pass.
- [ ] Billing distinguishes billable, syncing, failed, confirmed, and legacy states.
- [ ] Provider, store, worker, API, UI, and responsive browser tests pass.

---

## Phase 3: Atomic Seat Capacity

**User stories**: A workspace cannot exceed its plan by accepting stale or concurrent invitations,
and an administrator understands which lifecycle action frees a seat.

### What to build

Recheck the current plan member cap under a tenant lock immediately before both local magic-link
and SAML invitation acceptance. Return a stable plan-limit error and make member lifecycle copy
explicit about seat retention and release.

### Acceptance criteria

- [ ] Pending invitations remain unbilled and do not reserve capacity.
- [ ] Local invitation acceptance fails atomically when the plan is full.
- [ ] SAML invitation acceptance follows the same limit and locking rule.
- [ ] Concurrent acceptances cannot create more non-removed members than the plan allows.
- [ ] Deactivation is described as retaining a seat; removal is described as freeing one.
- [ ] Backend, web, and end-to-end regression tests pass.

---

## Completion Gate

- [ ] Backend full suite, web tests/typecheck, marketing tests/typecheck, and migration checks pass.
- [ ] Independent review finds no correctness, billing, security, or compatibility blocker.
- [ ] Main is integrated without unrelated workspace changes.
- [ ] CI and staging API/worker/web health are green.
- [ ] Staging browser and Stripe test-mode evidence is attached to #131 before closure.
