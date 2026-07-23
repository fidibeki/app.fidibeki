---
skill: review-chain
version: 1
date: 2026-07-23
status: final
---

# Review Chain Report

**Artifact**: Deterministic widget objective policy
**Date**: 2026-07-23
**Rounds**: 2 local review passes

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | minor | 10/10 | `widget/src/policy.ts` | Empty canonical and description tags increased the content-confidence score. | Fixed by requiring non-empty public metadata attributes, with a regression. |
| 2 | major | 9/10 | `widget/src/policy.ts` | Ordinary keyboard input counted as engagement even though the policy must avoid observing private input behavior. | Fixed by counting only keyboard activation actions (`Enter` and space); pointer interaction counts remain value-free. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | The approved roadmap defines objectives, allowed signals, presentation behavior, privacy boundaries, and compatibility. |
| Requirements clarity | Precise | Four objectives, deterministic local scoring, strict interruption policy, no private inputs/content/payloads/fingerprinting. |
| Upstream artifacts | Fresh | The shared runtime, recurrence, targeting, analytics, lifecycle, and Phase 3 audit are current. |

## Simplifications Applied

One pure evaluator owns scores, reason codes, and presentation thresholds. A small runtime observer supplies only
bounded counts and booleans. Existing targeting, recurrence, triggers, analytics, and explicit mounting remain
unchanged and compose with the new decision instead of being duplicated.

## Changes Made

Added objective-specific scoring, active-time visibility handling, canonical anonymous visit history, public
metadata and explicit boolean signals, launcher/inline non-interruption guardrails, stronger reaction/prompt
thresholds, automatic-loader composition, privacy regressions, and a real browser engagement journey.

## Reviewer's Summary

The policy is deterministic and fails closed for interruptive presentations. It never reads form values, page
text, network data, identity attributes, or browser entropy. Normal typing is ignored; active time pauses while
hidden; query strings, hashes, and trailing slashes share visit identity. Analytics keeps `matched` before the
decision and records `impression` only after a visible mount. No unresolved blocking issue remains.

## Resolver's Notes

Legacy definitions without the versioned objective/presentation contract bypass the new policy, preserving old
installations. Versioned automatic definitions retain their legacy trigger as an additional compatibility gate;
declarative and imperative mounts remain immediate because placement itself is an explicit host decision.
