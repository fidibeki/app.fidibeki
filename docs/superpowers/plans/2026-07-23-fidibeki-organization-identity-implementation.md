# Fidibeki Organization Identity Implementation

**Date:** 2026-07-23
**Status:** Active
**Scope:** Locked
**Source:** `docs/superpowers/specs/2026-07-23-fidibeki-organization-identity-design.md`
**Issues:** #30, #31
**Deployment:** Staging only

## Shared Context

- Extend tenant/member/widget appearance/storage/audit contracts already in the repository.
- Keep `tenants.name` as the public name and `widget_appearance_defaults.accent_color` as the
  organization accent source of truth.
- Store immutable, tenant-scoped PNG variants; never expose storage keys.
- Self avatar mutation only. Organization mutations require `tenant:update`.
- Preserve all project and widget overrides.

## Task 1: Forward-Compatible Profile Schema

**Depends on:** None

**Outcome:** Tenant and member records can hold organization profile and versioned identity metadata
without changing existing values.

**Acceptance:** Migration up/down/up tests prove defaults, constraints, RLS, grants, existing-row
preservation, cleanup-queue isolation, and compatible rollback.

**Autonomy:** AFK

## Task 2: Organization Profile Contract

**Depends on:** Task 1

**Outcome:** Authorized administrators can read and update normalized organization profile fields
with field errors and an immutable audit event.

**Acceptance:** Tenant integration tests prove successful update, clear-to-fallback, invalid fields,
permission denial, cross-tenant isolation, and before/after audit data.

**Autonomy:** AFK

## Task 3: Organization Defaults Inheritance

**Depends on:** Task 2

**Outcome:** Tenant session/config and public widget configuration expose current organization
defaults while preserving project and widget overrides.

**Acceptance:** Backend configuration tests prove environment-derived logo URLs and
organization -> project -> widget precedence without rewriting existing overrides.

**Autonomy:** AFK

## Task 4: Bounded Identity Image Processor

**Depends on:** Task 1

**Outcome:** PNG, JPEG, and WebP inputs are structurally decoded, bounded, cropped/fitted, and
normalized to deterministic 64/256 PNG variants.

**Acceptance:** Unit tests prove valid formats, extension/MIME mismatch, SVG, truncation, excessive
bytes/dimensions/pixels, avatar crop, and logo fit behavior.

**Autonomy:** AFK

## Task 5: Workspace Logo Lifecycle

**Depends on:** Tasks 2, 3, and 4

**Outcome:** Administrators can upload, replace, remove, and read a cache-busted workspace logo with
rate limiting, audit, and post-commit cleanup.

**Acceptance:** HTTP integration tests prove RBAC, tenant isolation, replacement/removal cleanup,
limiter behavior, storage failure preservation, public current-version reads, and stale-version
denial.

**Autonomy:** AFK

## Task 6: Member Profile And Avatar Lifecycle

**Depends on:** Tasks 1 and 4

**Outcome:** Every active member can edit their own display name and upload, replace, remove, and
read their own avatar while other-member mutation is denied.

**Acceptance:** HTTP integration tests prove self-service, impersonation denial, tenant isolation,
replacement/removal cleanup, rate limiting, storage failure preservation, and initials fallback
metadata.

**Autonomy:** AFK

## Task 7: Shared Identity UI

**Depends on:** Tasks 3, 5, and 6

**Outcome:** Settings, navigation, account profile, members, assignees, comments, mentions, and
activity use shared logo/avatar rendering with resilient initials fallbacks.

**Acceptance:** Web unit and Playwright tests prove immediate updates, field errors, read-only
controls, broken-image fallbacks, cache busting, and no desktop/mobile overflow.

**Autonomy:** AFK

## Task 8: Quality Gate And Staging

**Depends on:** Tasks 5, 6, and 7

**Outcome:** #30 and #31 are regression-tested and usable in staging with production untouched.

**Acceptance:** Migration replay, backend/web/widget suites, static checks, responsive browser
screenshots, CI, and one real staging profile/logo/avatar/widget inheritance journey pass for the
same commit.

**Autonomy:** AFK
