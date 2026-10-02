# Live support production release

Verified on 2026-10-01. Release `v0.2.0` points to
`67cf1c6e9270ed7d70fbcc621ce6e5b0065667bf`.
[Feature PR #176](https://github.com/fidibeki/app.fidibeki/pull/176) was merged
as `f788bf9c868fac4e97119721b839aacd7bd23b5c` after production verification.

## Deployment

[Production promotion 36906744841](https://github.com/fidibeki/app.fidibeki/actions/runs/36906744841)
passed, deploying the exact tested revision in this order: API, worker, widget,
backoffice, web. Railway reported `SUCCESS` for all five deployments:

| Service | Deployment |
|---|---|
| API | `1ae9164a-96cf-45ae-a79f-c0797548e8bd` |
| Worker | `2d522c2a-86fa-41d8-be07-48b253caf1f1` |
| Widget | `ab0a0807-376c-488d-a747-3437a6f8d915` |
| Backoffice | `28041119-183d-4574-bd09-035cb343709d` |
| Web | `f171ff92-b9f3-4363-b31a-9e323de260ef` |

The production migration command reported version 138 with `dirty=false`.
API readiness reported database, Redis and storage healthy. App, backoffice and
widget health endpoints returned HTTP 200. Recent API and worker deployment logs
contained no errors when inspected.

The first-party feedback configuration also needed repair: the widget URL now
includes `https://`, the app uses the existing Fidibeki project's current public
key, and that project's allowed origins include `*.fidibeki.com` alongside its
existing apex domain. Public configuration from the Tauview app origin returned
HTTP 200 with one active rule. No credentials are recorded here.

## Authenticated production smoke

The account owner signed into Chrome. Testing used their own Tauview account and
backoffice session. No other member was contacted. The two conversation messages
and one feedback submission are explicitly labeled as production smoke tests.

| Check | Observed result |
|---|---|
| Presence | Member appeared in Conversations without refreshing; active/idle and current page were visible; two app tabs counted as one member |
| Staff-only initiation | Empty member thread had no reply composer; sending the first staff message enabled replies |
| Staff delivery | Incoming preview appeared in the app without refreshing |
| Member reply | Reply appeared in the backoffice without refreshing |
| Persistence | Reloading the app retained both messages |
| Transcript | Chrome downloaded the JSON successfully; parsed artifact contained both exact smoke messages and `last_sequence=2` |
| Connection recovery | A transient reconnect notice during download cleared automatically |
| Feedback | Existing widget rendered under Give feedback; submitted labeled feedback and observed “Your feedback was sent.” |

## CI evidence

All feature checks passed on the deployed commit before promotion:

| Suite | Result |
|---|---|
| [Backend](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166079) | Full integration suite, unchanged 80% coverage gate and package floors, linter and image passed |
| [App](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166042) | 197 unit tests; 265 browser tests passed, 61 existing conditional skips; type check, build and image passed |
| [Widget](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166174) | 370 unit and 75 browser tests; type check, build and image passed |
| [Backoffice](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166348) | Unit/browser tests, type check, build and image passed |
| [Real support journey](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166020) | Real Go/Nuxt/widget bundles, PostgreSQL and Redis passed the isolated browser journey |
| [Security](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166091) | Secret scan and reachable Go vulnerability checks passed |
| [Mobile](https://github.com/fidibeki/app.fidibeki/actions/runs/36905166058) | Passed |
| [Marketing image](https://github.com/fidibeki/app.fidibeki/actions/runs/36905165988) | Passed |

The staging database and Redis deployments had previously been removed, so a
staging smoke could not run. The dormant environment was not recreated. The full
isolated real-stack journey passed before deployment, followed by the production
checks above. No capacity benchmark was performed.

Rollback instructions in [the runbook](../live-support.md) preserve the additive
support schema and conversation data.
