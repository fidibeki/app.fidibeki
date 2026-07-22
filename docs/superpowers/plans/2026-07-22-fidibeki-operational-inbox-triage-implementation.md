# Fidibeki Operational Inbox Triage Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-22-fidibeki-operational-inbox-triage-design.md`
**Scope:** Locked to product vertical slice 2
**Deployment:** Staging only; production remains untouched

## Shared Context

- Feedback and Issue are independent resources. A nullable `feedbacks.issue_id` is the only link.
- `priority` is a manual nullable value: `low`, `medium`, `high`, or `urgent`; `ai_priority` remains
  a recommendation.
- Feedback and Issue each own their assignee and tags. Values are copied only when an Issue is
  created from Feedback and never synchronize afterwards.
- Tags are tenant-wide. All resource, assignee, and tag boundaries are enforced by composite foreign
  keys and PostgreSQL RLS, not only by handler validation.
- Existing Issue creation uses `feedback:update` + `issue:update`; this slice preserves that RBAC
  contract rather than introducing a new permission.
- Every mutation uses the existing request-scoped tenant transaction. Bulk mutations are all-or-none.
- The Dispatch visual direction and current queue/inspector selection model remain authoritative.
- Each task follows one RED -> GREEN cycle through a public interface before the next task starts.

## Task 1: Persist Operational Triage Safely

**Depends on:** None

**Outcome:** Migration 46 adds manual priority and assignee columns plus tenant-scoped tags and join
tables without changing existing Feedback, Issue, or link records.

**Why:** Every API and UI behavior depends on database-enforced tenancy, deletion, and value rules.

**Acceptance:** `go test ./internal/db -run TestMigrationsEnforceTenantIsolation -count=1` proves valid
metadata persists, cross-tenant assignment/tagging fails, member deletion clears only assignees, tag
deletion clears assignments, RLS isolates the new tables, and existing links survive migration.

**Autonomy:** AFK

## Task 2: Expose Additive Feedback And Issue Read Models

**Depends on:** Task 1

**Outcome:** Feedback list/detail and Issue list/detail responses include nullable `priority`, a compact
assignee, and ordered tags while all existing fields remain unchanged.

**Why:** Later mutations and Dispatch controls need one stable representation of triage state.

**Acceptance:** A focused Go integration test reads seeded triage metadata through all four existing
HTTP read endpoints and receives empty additive fields for legacy records without breaking response
decoding.

**Autonomy:** AFK

## Task 3: Manage The Tenant Tag Catalogue

**Depends on:** Tasks 1-2

**Outcome:** Authenticated members can list tags and authorized writers can create, rename, recolor,
and delete them through `/v1/tags`.

**Why:** Controlled tenant vocabulary must exist before resource tag assignment can be usable.

**Acceptance:** A focused HTTP integration test completes the catalogue lifecycle, rejects invalid
names/colors, duplicate case-insensitive names and cross-tenant IDs, and confirms read-only members
cannot mutate it.

**Autonomy:** AFK

## Task 4: Mutate Feedback Triage Atomically

**Depends on:** Tasks 1-3

**Outcome:** The single Feedback PATCH supports manual metadata and `PATCH /v1/feedbacks/bulk` applies
status, priority, assignment, and tag deltas to 1-100 unique records in one transaction.

**Why:** This replaces the Dispatch `Promise.all` fan-out with a trustworthy bulk operation.

**Acceptance:** A focused HTTP integration test observes an atomic multi-record update through the
API and proves one invalid/cross-tenant input rolls back status, priority, assignee, and every tag
change.

**Autonomy:** AFK

## Task 5: Link And Unlink Feedback Explicitly

**Depends on:** Tasks 1-2

**Outcome:** `/v1/feedbacks/link` and `/v1/feedbacks/unlink` atomically manage 1-100 Feedback links,
with same-link and already-unlinked requests idempotent.

**Why:** Several independent reports must be organizable around one actionable Issue without data
loss.

**Acceptance:** A focused HTTP integration test links and unlinks several same-project records,
rejects mixed projects, merged targets, and conflicting links with the specified status codes, and
proves failed batches leave every link unchanged.

**Autonomy:** AFK

## Task 6: Create One Issue From Multiple Feedback Records

**Depends on:** Tasks 1-5

**Outcome:** `POST /v1/issues/from-feedback` creates one editable Issue from 1-100 records, applies
the approved priority/assignee/tag copy rules, links every record, and keeps the existing single-item
endpoint compatible through the same service.

**Why:** This is the core decision point from intake to actionable work.

**Acceptance:** A focused HTTP integration test creates an Issue from several Feedback records and
observes one public ID, one `issue.created` outbox event, correct copied metadata, correct reporter
semantics, and complete rollback on any conflict.

**Autonomy:** AFK

## Task 7: Edit Issue Triage Without Fabricating History

**Depends on:** Tasks 1-3

**Outcome:** `PATCH /v1/issues/{issueID}` becomes partial for title, description, priority, assignee,
tags, and optional status/comment while preserving existing status behavior.

**Why:** Actionable work needs editable ownership and classification after creation.

**Acceptance:** A focused HTTP integration test updates Issue metadata with no status-history row,
then changes status with exactly one history row and rejects invalid/cross-tenant metadata without
partial writes.

**Autonomy:** AFK

## Task 8: Add Typed Dispatch Triage Controls

**Depends on:** Tasks 2-4

**Outcome:** Shared web types and compact controls let an operator set priority, assignee, and tags
from the Feedback inspector while rows expose stable triage markers.

**Why:** The backend capability must become useful in the primary repeated-triage workflow.

**Acceptance:** One Playwright test edits all three fields from the inspector, verifies the persisted
row/inspector state after reload, and confirms users without `feedback:update` see read-only metadata.

**Autonomy:** AFK

## Task 9: Complete Bulk Inbox Triage

**Depends on:** Tasks 4-5 and Task 8

**Outcome:** The existing selection bar uses the atomic endpoint for priority, assignment, tags,
archive, and spam, keeps selection on failure, and uses a compact overflow menu on narrow screens.

**Why:** Operators need efficient, recoverable decisions across a queue rather than record-by-record
maintenance.

**Acceptance:** One desktop/mobile Playwright test applies each bulk command, forces one validation
failure to verify selection retention, and confirms the toolbar neither overlaps nor resizes rows.

**Autonomy:** AFK

## Task 10: Complete Create, Link, And Unlink Workflows

**Depends on:** Tasks 5-6 and Task 9

**Outcome:** Dispatch provides an editable create-Issue dialog, a project-restricted Issue picker,
unlink actions, explicit mixed-project disabled states, and direct navigation to the resulting Issue.

**Why:** This completes the intake-to-action transition inside the primary product surface.

**Acceptance:** One Playwright journey creates an Issue from multiple selected reports, unlinks and
relinks them to an existing Issue, and verifies mixed-project selections cannot submit create/link.

**Autonomy:** AFK

## Task 11: Surface Operational Metadata On Issues

**Depends on:** Tasks 6-7 and Task 8

**Outcome:** Issue list/detail show and edit priority, assignee, and tags using the same compact visual
language, with title/description editing retained separately from Feedback.

**Why:** Triage decisions must remain visible while the work progresses toward resolution.

**Acceptance:** One Playwright test edits Issue metadata, changes status, verifies the history contains
only the status transition, and confirms linked Feedback metadata did not synchronize implicitly.

**Autonomy:** AFK

## Task 12: Regress And Validate The Vertical Slice In Staging

**Depends on:** Tasks 1-11

**Outcome:** Backend, web, and widget remain compatible and the complete operational-triage journey
is proven in the staging tenant.

**Why:** The slice is complete only when real browser, API, database, and deployment behavior agree.

**Acceptance:** Full backend tests, web unit/type/build/Playwright suites, widget unit/type/build/E2E
suites, migration up/down/up, and one staging E2E journey all pass with captured desktop/mobile
screenshots; no production service or configuration is touched.

**Autonomy:** AFK
