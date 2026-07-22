# Fidibeki Operational Inbox Triage Design

**Date:** 2026-07-22
**Status:** Awaiting written review (domain decisions approved)
**Target:** Product vertical slice 2

## Problem

Feedback is now independent from Issues, but the inbox still cannot complete operational triage.
Operators can archive or mark individual records as spam and can create an Issue from one feedback,
but they cannot organize unlinked intake, associate several reports with the same work item, or apply
triage decisions atomically in bulk.

## Goals

- Give unlinked Feedback its own manual priority, assignee, and tags.
- Give Issues their own priority, assignee, and tags for execution tracking.
- Create one Issue from one or several Feedback records.
- Link and unlink one or several Feedback records without losing either resource.
- Apply status, priority, assignment, and tag changes to up to 100 Feedback records atomically.
- Preserve tenant, project, and RBAC boundaries in the database and HTTP contracts.
- Keep the Dispatch inbox dense, keyboard-readable, responsive, and useful for repeated triage.

## Non-goals

- Feedback and Issue triage metadata is not continuously synchronized.
- Linking to an existing Issue does not overwrite that Issue's metadata.
- This slice does not add comments, mentions, general activity history, or interactive Slack/Teams
  actions; those remain in later product slices.
- This slice does not change widget capture, AI request count, production, or infrastructure.

## Domain Model

### Priority And Assignment

`priority` has four stored values, ordered `low` < `medium` < `high` < `urgent`. A null value means
that an operator has not assigned a priority. This remains separate from `feedback.ai_priority`,
which is a recommendation and never silently becomes a manual decision.

Both `feedbacks` and `issues` gain nullable `priority` and `assignee_id` columns. Assignees reference
an existing member in the same tenant. The composite foreign key enforces the tenant boundary and
deleting a member clears only the assignment, not the Feedback or Issue.

### Tags

Tags are tenant-wide so the same controlled vocabulary works across projects. A tag contains a UUID,
tenant ID, case-insensitive unique name of 1-40 characters, and a `#RRGGBB` color. The UI uses a
restrained predefined palette but the API validates the stored color independently.

`feedback_tags` and `issue_tags` are separate tenant-scoped join tables with composite tenant/resource
and tenant/tag foreign keys, unique pairs, RLS, and cascade deletion. A tag assignment on one resource
does not mutate the other resource after Issue creation.

### Issue Creation Copy Rules

Creating an Issue from Feedback always receives editable title and description from the confirmation
form. Unless the operator changes the proposed values:

- Priority is the highest manual priority among the selected Feedback records. If none is manual, the
  highest existing AI suggestion may prefill the form but remains an explicit operator confirmation;
  creating the Issue does not make another paid AI call.
- Assignee is copied only when every selected Feedback has the same non-null assignee; otherwise it
  starts unassigned.
- Tags are the union of tags on the selected Feedback records.

These values are written to the new Issue in the same transaction that links the Feedback records.
Subsequent changes are independent. Existing single-feedback clients continue using
`POST /v1/feedbacks/{feedbackID}/issue`; it delegates to the same creation service.

## Mutation Contracts

### Bulk Feedback Triage

`PATCH /v1/feedbacks/bulk` accepts 1-100 unique Feedback UUIDs and at least one operation:

- `status`: `open`, `archived`, or `spam`;
- `priority`: `low`, `medium`, `high`, `urgent`, or `none` to clear;
- `assignee_id`, or `unassign: true` to clear;
- `add_tag_ids` and `remove_tag_ids`, which must be disjoint.

The endpoint requires `feedback:update`. It locks and validates every Feedback, assignee, and tag
before applying any change. Missing, cross-tenant, duplicate, or invalid resources reject the full
request; partial success is not allowed. The response reports the updated count and refreshed records.
The existing single-record PATCH remains compatible and may share the same validation/service layer.

### Link And Unlink

`POST /v1/feedbacks/link` accepts `feedback_ids` and `issue_id`.

- All Feedback records and the Issue must belong to the current tenant and one project.
- Feedback already linked to that same Issue is an idempotent no-op.
- Feedback linked to a different Issue causes `409 Conflict` and rolls back the full request.
- A merged Issue cannot receive new links.

`POST /v1/feedbacks/unlink` accepts `feedback_ids`. It clears existing links atomically and treats an
already-unlinked record as an idempotent no-op. Both endpoints require `feedback:update` and
`issue:update`.

`POST /v1/issues/from-feedback` creates an Issue from 1-100 Feedback records in one project. It accepts
editable title, description, priority, assignee, and tags, allocates one public ID, copies reporter
notification details only when the selected records have one unambiguous subscribed reporter, links
all records, and writes one `issue.created` outbox event. Any conflict rolls back the Issue, links,
counter allocation, and outbox event. It requires `feedback:update` and `issue:create`.

### Issue Triage

The existing `PATCH /v1/issues/{issueID}` remains the Issue mutation endpoint and becomes partial. It
accepts status/comment plus editable title, description, priority, assignment, and tag changes. Status
history is written only for a real status transition. Metadata-only changes do not fabricate a status
history entry. The endpoint requires `issue:update` and validates assignees/tags within the tenant.

### Tag Catalogue

- `GET /v1/tags` lists the tenant catalogue in name order for authenticated members.
- `POST /v1/tags` creates a tag for members with `feedback:update`.
- `PATCH /v1/tags/{tagID}` renames or recolors a tag with `feedback:update`.
- `DELETE /v1/tags/{tagID}` deletes a tag with `feedback:update` and removes assignments through
  foreign-key cascades.

## Dispatch Experience

The existing queue, inspector, filters, and selection model remain the frame.

- Rows expose compact manual priority, assignee initials, and tag color markers without replacing the
  title, project, rating, evidence, link state, or age.
- The inspector gains an operational triage section with a priority segmented control, member menu,
  and tag picker. AI recommendations are visibly suggestions and can be adopted explicitly.
- Multi-select reveals commands for Create Issue, Link, Unlink, Priority, Assign, Tags, Archive, and
  Spam. Secondary commands live in a menu on narrow widths instead of overflowing the bar.
- Create Issue opens one editable confirmation form. Link opens an Issue picker restricted to the
  selected project. A mixed-project selection explains why create/link is disabled; other bulk triage
  remains available.
- Tag creation uses a name input and color swatches. Familiar Lucide icons label commands and every
  icon-only control has a tooltip and accessible name.
- Mobile keeps the list-first navigation. Bulk commands wrap into a stable two-row toolbar or menu;
  controls and labels must not overlap or resize queue rows.

## Errors And Concurrency

All mutations run inside the existing per-request tenant transaction. Stores lock resources in stable
UUID order to avoid deadlocks. Validation is completed before writes where practical. Expected errors
use the existing API envelope:

- `404` for a resource not visible in the current tenant;
- `409` for an existing link to a different Issue or another concurrent incompatible mutation;
- `422` for mixed projects, invalid IDs, invalid priority/color, contradictory operations, or more
  than 100 records;
- `403` for missing permissions or a read-only tenant.

The UI keeps the selection after an error so an operator can correct the action. On success it clears
the selection, refreshes the queue, and shows the actual affected count.

## Migration And Compatibility

Migration 46 is additive: create the catalogue/join tables and add nullable, check-constrained
`priority` columns. Existing Feedback, Issues, links, public IDs, and status history remain unchanged.
Down migration removes only the new objects. Migration tests cover up/down/up, RLS, cross-tenant
assignment/tag rejection, member deletion, tag deletion, and preservation of existing links.

Existing response fields remain compatible. New `priority`, `assignee`, and `tags` fields are additive.
The single-feedback Issue creation endpoint and existing status mutations remain valid.

## Verification

- Backend unit tests cover priority ordering, request validation, copy rules, and error mapping.
- PostgreSQL integration tests cover atomic bulk updates, same-project link/create, idempotent
  link/unlink, conflicting links, merged Issues, RLS, composite foreign keys, and rollback behavior.
- Web unit/type tests cover the additive API contract and triage helpers.
- Dispatch Playwright tests cover single and bulk triage, tag creation, Issue creation from several
  Feedback records, linking, unlinking, permission-restricted controls, mixed-project validation, and
  desktop/mobile layout screenshots.
- Full backend, web, and widget compatibility suites and builds pass before deployment.
- Staging E2E creates unlinked Feedback, applies bulk triage, creates and links an Issue, unlinks and
  relinks records, and verifies persisted priority, assignee, and tags through both API and UI.

Production remains untouched until explicitly authorized.
