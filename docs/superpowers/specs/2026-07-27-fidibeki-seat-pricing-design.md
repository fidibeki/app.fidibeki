# Fidibeki Seat Pricing Design

**Date:** 2026-07-27
**Status:** Approved
**Issue:** [#131](https://github.com/fidibeki/app.fidibeki/issues/131)
**Target:** Staging product development

## Goal

Make the published Starter, Growth, and Scale amounts genuine per-seat prices from marketing
through Stripe and the workspace billing view. A seat is tied to workspace membership, so the
quantity charged by Stripe follows the number of billable members without requiring a customer to
maintain two separate counts.

This is a billing-contract change, not a copy-only change. Showing `/seat` while every checkout
still buys one flat plan would be misleading.

## Decisions

### Unit prices and plan limits

The existing amounts become unit prices:

- Starter: `$9.99` per seat each month, or `$111.48` per seat each year;
- Growth: `$29.99` per seat each month, or `$309.48` per seat each year;
- Scale: `$67.99` per seat each month, or `$701.64` per seat each year;
- Enterprise remains custom.

The existing plan member limits remain in force: 1, 3, and 10 members respectively. Seat pricing
does not remove the product and usage differences between plans.

The plan catalog exposes `pricing_model: per_seat` and `price_unit: seat`. Marketing data mirrors
this contract and its existing parity test continues to prevent catalog drift.

### What consumes a seat

A billable seat is a `tenant_members` row whose status is not `removed`.

- Active members consume a seat.
- Deactivated members retain a seat because their identity and workspace membership are retained.
- Removed members stop consuming a seat.
- Pending invitations do not reserve or consume a seat.
- Every workspace has a minimum Stripe quantity of one.

The member and billing interfaces state that removal frees a seat while deactivation does not.

### Existing subscriptions

Existing Stripe subscriptions must never be repriced merely because this code is deployed.

A new tenant billing model distinguishes:

- `legacy_flat`: a subscription that existed before the migration and remains at quantity one;
- `per_seat`: a new subscription whose quantity follows billable workspace members.

The migration marks every tenant with an existing subscription as `legacy_flat`. Tenants without a
subscription use `per_seat`, including existing trials and all future signups. A legacy
subscription is labelled clearly in the workspace billing view and is excluded from automatic seat
synchronization. Converting legacy customers later requires an explicit, separately approved
migration.

### Checkout and provider state

Checkout reads the live billable-member count and sends `max(1, count)` as the Stripe line-item
quantity. Stripe metadata continues to identify the tenant.

The Stripe provider contract exposes:

- checkout quantity;
- subscription item ID and quantity on retrieval;
- an idempotent subscription-item quantity update with prorations enabled.

Webhook processing records the quantity observed from Stripe. It remains the authoritative
confirmation that provider state changed; local membership remains authoritative for the desired
quantity.

### Self-healing synchronization

The worker periodically selects per-seat subscriptions where:

`provider-confirmed quantity != max(1, current billable members)`

For each mismatch it updates the single Stripe subscription item and records success or a bounded
error message. The update uses an idempotency key derived from tenant, previous quantity, and
desired quantity. A membership change during a provider call is safe: the applied remote quantity
is recorded, the next scan observes any new mismatch, and convergence continues.

This source-derived reconciliation is preferred for the first slice over a second event queue:

- it cannot lose a membership event;
- it covers invitation acceptance, SAML acceptance, removal, and administrative creation paths;
- retries survive process restarts;
- the current customer scale does not justify a separate dirty-row queue.

The query is bounded and indexed. A later event-driven wake-up may reduce scan work without changing
the reconciliation contract.

The billing view exposes current billable seats, Stripe-confirmed seats, estimated recurring total,
and a syncing or failed state. It never claims that an unconfirmed provider update succeeded.

### Invitation limit correctness

The existing invitation endpoint checks the member limit before sending an invitation, but an old
or concurrent invitation can currently be accepted after the plan is full.

Both local magic-link acceptance and SAML invitation acceptance will:

1. lock the tenant row;
2. resolve the current plan limit;
3. count non-removed members in that transaction;
4. reject creation when the limit is already reached.

Pending invitations remain free and do not reserve capacity. Acceptance is the atomic boundary at
which a seat is created.

## Alternatives Considered

### Presentation-only seat wording

Rejected. Stripe would keep charging one flat quantity and the public price would not describe the
actual invoice.

### Customer-managed Stripe quantity

Rejected. Members and billed seats would drift, invite and removal flows would not have predictable
billing behavior, and customers would have to perform the same operation twice.

### Immediate conversion of all subscriptions

Rejected. It could create an unannounced mid-cycle charge. Legacy subscriptions stay flat until a
separate customer migration is explicitly approved.

### Membership events as the only synchronization source

Rejected for this slice. Missing one write path or losing one event would leave Stripe permanently
wrong. Periodic reconciliation derives desired quantity from durable source data and self-heals.

## Data Model

The tenant billing record adds:

- `billing_model`;
- `billing_seat_quantity`, last confirmed by Stripe;
- `billing_seat_sync_attempted_at`;
- `billing_seat_synced_at`;
- `billing_seat_sync_error`.

The migration:

1. adds the fields with safe defaults;
2. marks current subscriptions `legacy_flat`;
3. initializes their confirmed quantity to one;
4. initializes per-seat tenants from their current non-removed member count;
5. grants only the column access required by the application role;
6. supports down/up migration without changing membership or subscription identifiers.

No personal data is added.

## API And UI Contract

The plan catalog includes pricing model and unit. The tenant billing summary adds:

- billing model;
- billable seat count;
- confirmed subscription quantity;
- seat synchronization status and timestamps;
- unit price and estimated total for the active interval.

The additions are backward compatible. Existing fields remain unchanged.

Marketing, signup, and billing surfaces render unit and total values explicitly. Structured offer
metadata continues to publish the unit price rather than a hypothetical maximum-plan total.

## Failure Handling

- An unavailable Stripe provider leaves the mismatch pending and records a safe operator-facing
  error.
- Provider errors do not roll back membership changes.
- Webhook replay remains idempotent through the existing Stripe event ledger.
- Unknown, missing, or multiple Stripe subscription items fail closed and remain visible for retry;
  the reconciler never guesses which item to mutate.
- A stale reconciliation result cannot suppress a newer mismatch because desired quantity is
  recalculated on every bounded scan.
- Legacy subscriptions are never mutated by the reconciler.

## Validation

### Database and backend

- Migration up/down/up and data-preservation tests.
- Plan catalog unit and JSON contract tests.
- Checkout sends the live seat quantity.
- Stripe retrieval and update map item ID, quantity, proration, and idempotency correctly.
- Webhooks persist provider-confirmed quantity.
- Reconciliation covers add/remove changes, retry, stale membership changes, and legacy exclusion.
- Local and SAML invitation acceptance reject a full plan inside the transaction.
- Existing billing lifecycle and downgrade tests remain green.

### Frontend and marketing

- Pricing parity tests cover model and unit.
- Billing totals and legacy/sync states have unit tests.
- Signup and pricing pages identify per-seat prices.
- Responsive browser checks cover pricing and billing at desktop and mobile widths without overflow.

### Staging

- New checkout line-item quantity matches workspace seats.
- Adding or removing a member converges Stripe and billing summary.
- Deactivation retains the seat; removal frees it.
- Legacy fixtures, if present, remain quantity one and show the legacy label.
- API and worker health remain green.

No production deployment or production subscription conversion is part of this issue.
