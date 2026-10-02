# Search discovery and competitor content

Reviewed 2 October 2026. This records shipped work and the evidence still needed to measure search performance.

## What ships

- 44 marketing routes in English, European Portuguese, Spanish and French, producing 176 HTML pages and matching Markdown representations.
- Localized titles, descriptions, canonical URLs, reciprocal language links, structured data and a generated sitemap. Genuine unknown URLs return 404.
- A homepage description that names the product category: website feedback and live chat in one widget. The approved interactive homepage remains intact.
- `/compare` and eight distinct competitor pages, linked from the footer, related feature pages and each other. Versus pages compare tasks and pricing. Alternative pages address fit, evaluation and migration limits.
- `/llms.txt` links to generated Markdown, including the comparison directory and versus pages. Markdown is a representation of published page content, not a separate collection of search-only claims.

## Search intent and destination

| Intent | Existing destination |
| --- | --- |
| Website feedback widget | `/`, `/features/feedback-widget` |
| Website visitor conversations | `/`, `/use-cases/customer-support` |
| Session replay with feedback | `/features/session-replay` |
| Feedback issue tracking | `/features/issue-tracking` |
| Notify a feedback reporter | `/features/reporter-loop` |
| Marker.io alternative | `/marker-io-alternative` |
| Userback alternative | `/userback-alternative` |
| BugHerd alternative | `/bugherd-alternative` |
| Canny alternative | `/canny-alternative` |
| Direct product comparisons | `/fidibeki-vs-marker-io`, `/fidibeki-vs-userback`, `/fidibeki-vs-bugherd`, `/fidibeki-vs-canny` |

These are intent mappings, not measured keyword volumes. Prefer improving a relevant existing feature page over creating another page for a minor wording variation.

## Evidence for the comparisons

`data/comparisons.ts` centralizes official source URLs, review dates, pricing cadence and content. Fidibeki prices and included quantities are read from `data/pricing.ts`, which is tested against the backend plan catalog.

The comparison is based on published documentation, not a hands-on benchmark. There are no invented switcher quotes, ratings, migration tools or savings percentages. Userback and Canny free plans are explicit. Marker.io and Canny annual billing is labeled. BugHerd and Userback prices use the monthly option. The pages explain limits of substituting private feedback for annotation, design review, public voting or a public roadmap.

Review sources quarterly, next due 2 January 2027, and whenever a vendor changes a relevant price or feature. Update the English source, all three translations and the review date together. Do not advance the date without checking the sources.

## What is not measured yet

There is no connected Google Search Console or backlink dataset in this workspace. Search impressions, positions, indexed-page counts, referring domains and non-branded traffic are therefore unknown. A public search result or absence from one query cannot establish those metrics. Technical checks cannot guarantee indexing, ranking or AI citations.

When Search Console access is available:

1. Verify the `fidibeki.com` property and submit `https://fidibeki.com/sitemap.xml` if needed.
2. Inspect the homepage, one feature page, each comparison family and one localized page. Check Google's selected canonical and indexing status.
3. Save a 28-day baseline by page, query, country and device. Separate branded searches from product-category and competitor searches.
4. After sufficient impressions accumulate, inspect weak click-through rates and query/page mismatches before rewriting titles. Measure trial and demo outcomes alongside search visits using the existing consent-based analytics.

## Next content worth producing

Use actual product examples and verify every procedure in the app before publishing:

- How to collect a reproducible mobile bug report with screenshots and consented replay.
- How to connect several feedback reports to one issue and update the reporters after a fix.
- When a private feedback inbox and a public feature-voting board serve different needs.

These are a content backlog, not pages claimed as published. No additional competitor names, duplicate keyword pages or unverified customer case studies are necessary to support the current comparison set.

For authority, a real customer case study and useful integration guides offer material others can reference. External outreach, directory submissions and customer endorsements require their own authorization and evidence. None were sent or invented during this work.

### Conversation-led positioning, 2 October 2026

The product now links visitor messages to issues, with a return path for manual chat replies. `/features/website-conversations` explains this implemented workflow and links to issue tracking and the separate opted-in reporter email flow. It is available in all four locales and included in generated canonical/hreflang metadata, sitemap, Markdown and `llms.txt`. Comparison tables now start with conversation and follow-up, before discussing prices. Canny comparisons acknowledge private boards and roadmaps.

Evaluate this positioning through trial activation and customer interviews; no ranking or conversion improvement has been measured. A useful activation event to define is completing the first visitor message → issue → reply journey. Existing analytics do not yet measure that full product journey.
