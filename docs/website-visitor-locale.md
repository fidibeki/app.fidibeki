# Visitor browser language

The tenant Conversations screen shows the visitor's preferred browser language in the online list, inbox and selected conversation, for example Portuguese (Angola), `pt-AO`. Display names follow the operator's app language. There are no country flags or claims of verified location.

The widget reads the first usable `navigator.languages` entry, falling back to `navigator.language`. It retains only language, script and region, removing variants and private extensions. Host-page language and widget UI language do not override this preference. An unavailable preference leaves chat usable.

Session creation and presence accept the optional `locale` field. Migration 142 adds a bounded field to the project-scoped visitor record. Presence refreshes it only when a nonempty preference changes; it does not update the conversation timestamp or extend retention. Older widgets can omit it without erasing a known value. It remains available offline and is deleted with the visitor conversation. Existing permissions, tenant isolation and RLS still apply.

Validation covers normalization, invalid payloads, stored session language, presence changes, legacy omission, unchanged ordering timestamps, actual widget requests, and desktop/mobile operator display. The real API/browser journey uses a Portuguese browser on an English website and verifies the operator sees `pt-PT`.

Evidence from the real widget/API/operator journey: [desktop](evidence/visitor-locale-desktop.png), [mobile](evidence/visitor-locale-mobile.png). Backend integration, 376 widget unit tests, widget/web typechecks, 8 operator browser tests and both live-support integration journeys passed.
