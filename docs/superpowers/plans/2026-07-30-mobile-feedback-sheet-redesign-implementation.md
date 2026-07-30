# Fidibeki Mobile Feedback Sheet Redesign Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-30-mobile-feedback-sheet-redesign-design.md`
>
> **Boundary:** local and staging-only mobile product work. No backend, production, DNS, provider,
> infrastructure, or package-registry changes.

## Task 1: Restore Dismiss And Reopen Semantics

Start with failing recurrence and provider component tests. Prove that closing an open sheet does
not write storage, change eligibility, acquire another capture session, or discard the current
answers/screenshot. Reopening must reuse the current session and draft. Treat a stored
`dismissed` record from previous SDK versions as invalid legacy state: remove it and return eligible
for session, cooldown, and never definitions.

Narrow new recurrence writes to accepted submissions. Keep legacy `dismissed` parsing only long
enough to perform the storage cleanup. Preserve session expiry, configuration refresh, background
session renewal, and submitted recurrence behavior.

**Primary files:** `mobile/src/provider.tsx`, `mobile/src/recurrence.ts`,
`mobile/tests/components.test.tsx`, `mobile/tests/recurrence.test.ts`.

**Acceptance:** One focused Vitest run proves close -> reopen with the same session/draft and zero
recurrence writes, legacy dismissed cleanup under all recurrence modes, and unchanged successful
submission suppression.

## Task 2: Replace The Page Form With The Compact Sheet

Add failing component assertions for the agreed frame and rating semantics. Replace
`presentationStyle="pageSheet"` with a custom bottom-aligned modal surface using existing React
Native primitives: scrim, 75% initial safe height, grabber, compact fixed header, scrolling form
body, and fixed submit footer. Track keyboard visibility so the sheet expands to the full safe
height available above the keyboard and returns to 75% afterward. Preserve reduced-motion,
safe-area, orientation, and dismissal behavior without adding a native module or bottom-sheet
dependency.

Render rating as five cumulative star radio targets. A value of four fills the first four stars,
uses the resolved accent for every filled state, exposes localized `n out of 5` accessibility
labels, and still writes numeric `4` to the form. Restyle reaction and single-choice controls as
compact wrapping chips. Keep every configured response block visible and in definition order.

**Primary files:** `mobile/src/sheet.tsx`, optional internal sheet presentation module,
`mobile/src/localization.ts`, `mobile/tests/components.test.tsx`.

**Acceptance:** One focused iOS/Android component run proves 75%/keyboard geometry, stable
header/footer, cumulative accent stars with numeric submission, dynamic block order, chip states,
safe areas, and reduced motion.

## Task 3: Add Progressive Optional Sections And Complete States

Start with failing component tests for the approved disclosure behavior. Keep customer-configured
response blocks visible. Render `allow_comment` behind **Add more context**, retaining its value
across collapse/reopen. Render valid contact collection behind **Get updates** and keep that section
open once activated so consent is never silently hidden. Keep the AI disclosure visible when
applicable. Present screenshot add/attached/remove as compact action rows while retaining the
host-owned provider boundary and all current disposal/error behavior.

Polish free-text sizing, required/error treatment, loading, retry, accepted-feedback success, and
attachment-warning states. Scroll/focus the first invalid control when feasible. Ensure long copy,
Dynamic Type, light/dark mode, and custom accents cannot overlap or escape their containers.

**Primary files:** `mobile/src/sheet.tsx`, optional internal sheet presentation module,
`mobile/src/localization.ts`, `mobile/tests/components.test.tsx`,
`mobile/tests/session-form.test.ts`.

**Acceptance:** One focused component/form run proves disclosure state and data safety, screenshot
states, error focus, retry draft preservation, success/attachment warnings, long copy, dark mode,
and accessible expanded/selected/busy states.

## Task 4: Prove The Native Experience End To End

Update the example fixture only where needed to exercise every visual state; keep the host-owned
emoji trigger and no SDK launcher. Run the complete mobile unit coverage, typecheck, package build,
dry-run package check, and example typecheck. Launch the example against its local smoke server on
an iOS simulator and Android emulator or equivalent available runtime.

Capture and inspect light/dark screenshots on a compact and standard viewport. Exercise host
trigger open, close, reopen with the same draft, rating/choice/text input, screenshot attachment,
keyboard expansion, submit, success, and post-submit recurrence. Confirm the SDK makes no extra
session request on reopen and no forbidden permission, identifier, analytics, backend, or
production change is introduced. Update mobile documentation and the execution record with exact
evidence.

**Primary files:** `examples/mobile/App.tsx`, `examples/mobile/smoke-server.mjs`,
`mobile/README.md`, `docs/mobile-capture.md`, `.agents/tasks.md`.

**Acceptance:** All mobile/package/example gates pass, inspected iOS/Android runtime evidence
matches the approved sheet without overlap, and the tested commits contain no backend or
production changes.
