# @fidibeki/marketing

The static, prerendered marketing site for [fidibeki.com](https://fidibeki.com). Built with
Nuxt 3 (matching the `web` workspace), generated to plain HTML in `.output/public`, and
deployable to any static host.

## Commands

From the repository root:

```sh
pnpm build:marketing        # static HTML + sitemap.xml + Markdown pages
pnpm dev:marketing          # local dev server
pnpm test:marketing         # Vitest unit tests
pnpm test:marketing:e2e     # Playwright against the static build (run build first)
pnpm typecheck:marketing
```

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `NUXT_PUBLIC_SITE_URL` | `https://fidibeki.com` | Canonical origin used for metadata and the sitemap. |
| `NUXT_PUBLIC_APP_URL` | `https://fidibeki.com` | Where sign-in (`/login`) and sign-up (`/signup`) link. The app currently serves signup on the base domain; point this elsewhere if the app moves. |
| `NUXT_PUBLIC_DEMO_SCHEDULER_URL` | `https://cal.eu/acidiney.dias-fidibeki/30min` | External scheduling link — every "Book a demo" button opens it directly in a new tab. |
| `NUXT_PUBLIC_UMAMI_SCRIPT_URL` | `https://cloud.umami.is/script.js` | Umami analytics script location. |
| `NUXT_PUBLIC_UMAMI_WEBSITE_ID` | `1d518b00-…` | Umami website id. Analytics load only after the visitor accepts the consent banner; set empty to disable entirely. |
| `NUXT_PUBLIC_WIDGET_SCRIPT_URL` | `https://widget.fidibeki.com/v1/loader.js` | Fidibeki widget loader script location. |
| `NUXT_PUBLIC_WIDGET_KEY` | _(unset)_ | Publishable key for the Fidibeki widget instance running on this site. No default: unset means no widget. Loads only after the visitor accepts the consent banner. |
| `NUXT_PUBLIC_WIDGET_CONSENT_LOCALE` | `en` | Locale asserted in the widget consent grants; must be one of the widget rule's supported locales. |
| `NUXT_PUBLIC_WIDGET_ANALYTICS_NOTICE_VERSION` | `2026-07` | Notice version asserted when the banner grants `widget_analytics` to the widget; must match the widget rule's customer_cmp analytics consent configuration. |
| `NUXT_PUBLIC_WIDGET_REPLAY_NOTICE_VERSION` | `2026-07` | Notice version asserted when the banner grants `session_replay` to the widget; must match the widget rule's customer_cmp replay consent configuration. |
| `NUXT_PUBLIC_STATUS_PAGE_URL` | `https://status.fidibeki.com` | Public status page. Used as the status badge's link target, and as the origin its live-status read goes to. |
| `NUXT_PUBLIC_STATUS_PAGE_ID` | _(unset)_ | Status page id the badge reads live status for. No default and never committed: unset means the badge stays a plain link instead of showing a status it cannot verify. Find it in the status page's own `POST /status-page-api/overview/<id>` request. |

The footer LinkedIn icon is an explicit placeholder — set `linkedInUrl` in
`components/SiteFooter.vue` when a real profile exists.

## Content model

All page content is repository-backed TypeScript in `data/`:

- `features.ts`, `integrations.ts`, `use-cases.ts`, `docs.ts` drive the extensible
  `/features/*`, `/integrations/*`, `/use-cases/*`, and `/docs/*` routes.
- `pricing.ts` mirrors `backend/internal/config/plans.go`. A unit test
  (`tests/unit/pricing-sync.test.ts`) parses the Go plan catalog and fails if the two drift —
  never edit one without the other.
- `site.ts` owns navigation, the loop copy, and the canonical route list used for
  prerendering, the sitemap test, and the broken-link test.

## Testing

- **Unit (Vitest):** content integrity (slugs, cross-references, required IA routes),
  backend pricing sync, translation keys and parameters, sitemap and Markdown generation.
- **E2E (Playwright, desktop/tablet/mobile):** raw-HTML metadata and structured data on every
  route, broken internal links, sitemap/robots correctness, navigation, theme persistence and
  no-flash behavior, active navigation after one click, language switching, keyboard/landmark accessibility checks,
  horizontal-overflow checks per viewport and locale, and the 404 page.

E2E runs against the exact static output via `scripts/serve-static.mjs` (gzip + immutable
asset caching, mirroring production delivery).

## Validation record (2026-07-23)

Lighthouse 12, mobile emulation, against the compressed static build:

| Page | Performance | Accessibility | Best practices | SEO |
| --- | --- | --- | --- | --- |
| `/` | 97 | 100 | 100 | 100 |
| `/pricing` | 95 | 100 | 100 | 100 |
| `/docs/installation` | 96 | 100 | 100 | 100 |
| `/features/session-replay` | 95 | 100 | 100 | 100 |

## Validation record (2026-10-02)

- 82 unit tests and Nuxt type checking passed.
- The prerendered build contains 140 localized HTML pages and 140 Markdown pages.
- Playwright passed 128 tests; 12 viewport-specific cases were intentionally skipped.
  Coverage includes all localized metadata and Markdown, pricing translations,
  320–1440px layouts, first-click navigation and language changes without a reload.
- Caddy configuration and local HTTP checks passed for localized routes, Markdown
  content types, the discovery index, health checks and genuine 404 responses.

## Notes for the founder

- Prices, limits, and feature gates shown on `/pricing` come from the backend plan catalog
  and are covered by tests; Enterprise is intentionally "Custom".
- `/privacy` and `/security` describe implemented behavior only; `/security` explicitly states
  that no certification is claimed. Publish counsel-reviewed policy documents when they exist.
- The social preview image is generated by `node scripts/generate-og.mjs`.

## Languages and SEO

`@nuxtjs/i18n` publishes 35 routes in four languages: English at the original URLs,
European Portuguese under `/pt`, Spanish under `/es`, and French under `/fr`. The
language selector keeps the route, query and anchor and remembers the choice in
`fidibeki-locale`.

`useMarketingI18n().copy()` maps the English source string to its stable key in
`i18n/locales/{en,pt,es,fr}.json`. Add every new message to all four catalogs. Use
complete sentences with named parameters, including separate singular and plural
messages. Do not build translated sentences by joining English fragments. Format
quantities through `copy(number)`. Keep code samples and user-entered content intact.

`useSiteMeta()` supplies translated titles, descriptions, social metadata, structured
data, canonical URLs and Markdown discovery links. Pass a function when the options
themselves contain reactive translated values. Locale-aware head links supply
reciprocal `hreflang` entries, including `x-default`. The sitemap lists all 140 pages.

After prerendering, `scripts/generate-markdown.mjs` extracts page content from the
rendered HTML and writes matching `.md` files, including `/index.md` and localized
`/pt/index.md` equivalents. It keeps documentation code and comparison tables,
removes interactive controls, and connects known internal pages through Markdown
links. Do not edit generated Markdown. Update the source page or translation.

`public/llms.txt` is the curated discovery index, following [llmstxt.org](https://llmstxt.org/).
Keep its links and product summary current when routes or capabilities change. The
production Caddy config serves Markdown as `text/markdown`; HTML links expose both
the alternate representation and `/llms.txt`. All supported pages remain crawlable
in `robots.txt`. These additions do not guarantee indexing or AI citations.

The localization and SEO Playwright suites validate every prerendered page, its
Markdown representation, metadata, canonical and language links. They also exercise
client-side language changes so rendered content and metadata stay in sync.
