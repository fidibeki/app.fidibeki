<!--
THESIS: Help a team choose with evidence, then test the workflow that matters to them.
OWN-WORLD: Inherit the approved neutral typography, open reading layout and compact controls.
STORY: Understand fit, compare the actual work and costs, then evaluate a trial or keep the existing tool.
FIRST VIEWPORT: Shared header, clear comparison title, a short verdict and trial/source navigation on the established container.
FORM: Extend the existing product-detail and pricing pages. Alternatives explain switching; versus pages compare the same task in both products.
-->
<script setup lang="ts">
import { ArrowRight, ArrowUpRight } from '@lucide/vue'
import {
  comparisonForPath,
  alternativePath,
  versusPath,
} from '~/data/comparisons'
import { plans, formatPrice } from '~/data/pricing'
import { unlocalizedPath } from '~/utils/locale'

const { copy, locale } = useMarketingI18n()
const route = useRoute()
const entry = comparisonForPath(unlocalizedPath(route.path))
if (!entry)
  throw createError({
    statusCode: 404,
    statusMessage: 'Page not found',
    fatal: true,
  })
const { competitor, kind } = entry
const isAlternative = kind === 'alternative'
const links = useAppLinks()
const title = computed(() =>
  isAlternative
    ? copy(competitor.alternative.title)
    : copy('Fidibeki vs {competitor}', { competitor: competitor.name }),
)
const pagePath = isAlternative
  ? alternativePath(competitor.slug)
  : versusPath(competitor.slug)
const reviewed = computed(() =>
  new Intl.DateTimeFormat(locale.value === 'pt' ? 'pt-PT' : locale.value, {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(new Date(`${competitor.reviewedAt}T12:00:00Z`)),
)
const starter = plans.find((plan) => plan.id === 'starter')!
const growth = plans.find((plan) => plan.id === 'growth')!
const sources = competitor.sources
const pricingSource = sources.find((source) => source.label === 'Pricing')!
useSiteMeta(() => ({
  title: isAlternative
    ? competitor.alternative.title
    : copy('Fidibeki vs {competitor}: features, pricing and fit', {
        competitor: competitor.name,
      }),
  description: isAlternative
    ? competitor.alternative.description
    : competitor.versus.description,
  path: pagePath,
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Compare', path: '/compare' },
    { name: title.value, path: pagePath },
  ],
  structuredData: [
    {
      '@context': 'https://schema.org',
      '@type': 'Article',
      headline: title.value,
      description: isAlternative
        ? competitor.alternative.description
        : competitor.versus.description,
      author: { '@type': 'Organization', name: 'Fidibeki' },
      dateModified: competitor.reviewedAt,
    },
  ],
}))
</script>

<template>
  <div class="comparison-page">
    <PageHero
      :eyebrow="copy('Compare')"
      :title="title"
      :lede="
        copy(
          isAlternative
            ? competitor.alternative.introduction
            : competitor.versus.verdict,
        )
      "
    >
      <template #breadcrumbs
        ><Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Compare', path: '/compare' },
            { name: competitor.name, path: pagePath },
          ]"
      /></template>
      <template #actions>
        <a :href="links.signUp" class="btn btn-primary"
          >{{ copy('Try Fidibeki') }}
          <ArrowUpRight :size="16" aria-hidden="true"
        /></a>
        <NuxtLinkLocale
          :to="
            isAlternative
              ? versusPath(competitor.slug)
              : alternativePath(competitor.slug)
          "
          class="btn btn-ghost"
          >{{
            copy(
              isAlternative
                ? 'See the full comparison'
                : 'Considering a switch?',
            )
          }}
          <ArrowRight :size="16" aria-hidden="true"
        /></NuxtLinkLocale>
      </template>
    </PageHero>
    <div class="container comparison-body">
      <nav class="comparison-contents" :aria-label="copy('On this page')">
        <a :href="isAlternative ? '#fit' : '#workflow'">{{
          copy(isAlternative ? 'Who it fits' : 'Compare the workflow')
        }}</a>
        <a href="#pricing">{{ copy('Pricing') }}</a>
        <a :href="isAlternative ? '#switching' : '#evaluate'">{{
          copy(isAlternative ? 'Planning a switch' : 'What to test')
        }}</a>
        <a href="#sources">{{ copy('Sources') }}</a>
      </nav>

      <template v-if="isAlternative">
        <section id="fit" class="comparison-section fit-section">
          <div>
            <h2>{{ copy('When Fidibeki fits') }}</h2>
            <p>{{ copy(competitor.alternative.fit) }}</p>
            <NuxtLinkLocale
              to="/features/feedback-widget"
              class="comparison-link"
              >{{ copy('Explore the feedback widget') }}
              <ArrowRight :size="16" aria-hidden="true"
            /></NuxtLinkLocale>
          </div>
          <div>
            <h2>
              {{
                copy('When to keep {competitor}', {
                  competitor: competitor.name,
                })
              }}
            </h2>
            <p>{{ copy(competitor.alternative.keep) }}</p>
            <a
              :href="competitor.sources[0]!.url"
              class="comparison-link"
              rel="noopener"
              target="_blank"
              >{{
                copy('Explore {competitor}', { competitor: competitor.name })
              }}
              <ArrowUpRight :size="16" aria-hidden="true"
            /></a>
          </div>
        </section>
      </template>
      <section v-else id="workflow" class="comparison-section">
        <h2>{{ copy('Compare the workflow') }}</h2>
        <p id="comparison-scroll-hint" class="comparison-scroll-hint">
          {{ copy('Scroll sideways to compare both tools.') }}
          <ArrowRight :size="16" aria-hidden="true" />
        </p>
        <div
          class="comparison-table-wrap"
          tabindex="0"
          role="region"
          :aria-label="copy('Feature comparison')"
          aria-describedby="comparison-scroll-hint"
        >
          <table class="comparison-table">
            <caption>
              {{
                copy('How Fidibeki and {competitor} approach the same work', {
                  competitor: competitor.name,
                })
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ copy('Your task') }}</th>
                <th scope="col">Fidibeki</th>
                <th scope="col">{{ competitor.name }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in competitor.versus.rows" :key="row.topic">
                <th scope="row">{{ copy(row.topic) }}</th>
                <td>{{ copy(row.fidibeki) }}</td>
                <td>
                  {{ copy(row.competitor) }}
                  <a
                    :href="row.source"
                    rel="noopener"
                    target="_blank"
                    class="row-source"
                    :aria-label="
                      copy('{competitor} source: {topic}', {
                        competitor: competitor.name,
                        topic: copy(row.topic),
                      })
                    "
                    >{{ copy('Source') }}
                    <ArrowUpRight :size="12" aria-hidden="true"
                  /></a>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="comparison-details">
          <article v-for="row in competitor.versus.rows" :key="row.topic">
            <h3>{{ copy(row.topic) }}</h3>
            <div>
              <p>{{ copy(row.detail) }}</p>
              <NuxtLinkLocale :to="row.productPath" class="comparison-link"
                >{{ copy('See how Fidibeki handles this') }}
                <ArrowRight :size="16" aria-hidden="true"
              /></NuxtLinkLocale>
            </div>
          </article>
        </div>
      </section>

      <section id="pricing" class="comparison-section">
        <h2>{{ copy('Compare the plan you need') }}</h2>
        <p class="section-intro">
          {{
            copy(
              'Prices are in USD. Billing periods, included users and usage limits differ.',
            )
          }}
        </p>
        <div class="price-comparison">
          <div>
            <h3>Fidibeki</h3>
            <ul class="comparison-prices">
              <li>
                <strong
                  >Starter · {{ copy(formatPrice(starter.monthlyPriceCents))
                  }}{{ copy('/month') }}</strong
                ><span>{{ copy('Billed monthly') }}</span>
              </li>
              <li>
                <strong
                  >Growth · {{ copy(formatPrice(growth.monthlyPriceCents))
                  }}{{ copy('/month') }}</strong
                ><span>{{ copy('Billed monthly') }}</span>
              </li>
            </ul>
            <p>
              {{
                copy(
                  'Starter includes {projects} project, {members} team member and {reports} feedback submissions per month.',
                  {
                    projects: copy(starter.limits.projects),
                    members: copy(starter.limits.users),
                    reports: copy(starter.limits.feedbackPerMonth),
                  },
                )
              }}
            </p>
            <p>
              {{
                copy(
                  'Growth adds consented session replay and AI analysis, with {members} team members and {projects} projects.',
                  {
                    members: copy(growth.limits.users),
                    projects: copy(growth.limits.projects),
                  },
                )
              }}
            </p>
            <NuxtLinkLocale to="/pricing" class="comparison-link"
              >{{ copy('All Fidibeki plans and limits') }}
              <ArrowRight :size="16" aria-hidden="true"
            /></NuxtLinkLocale>
          </div>
          <div>
            <h3>{{ competitor.name }}</h3>
            <ul class="comparison-prices">
              <li v-for="plan in competitor.pricing" :key="plan.plan">
                <strong
                  >{{ plan.plan === 'Free' ? copy('Free') : plan.plan }} ·
                  {{ copy(formatPrice(plan.usd * 100))
                  }}{{ copy('/month') }}</strong
                ><span>{{
                  copy(
                    plan.usd === 0
                      ? 'Free plan'
                      : plan.cadence === 'year'
                        ? 'Monthly equivalent, billed annually'
                        : 'Billed monthly',
                  )
                }}</span>
                <p>{{ copy(plan.note) }}</p>
              </li>
            </ul>
            <p>{{ copy(competitor.pricingNote) }}</p>
            <a
              :href="pricingSource.url"
              rel="noopener"
              target="_blank"
              class="comparison-link"
              >{{
                copy('{competitor} pricing', { competitor: competitor.name })
              }}
              <ArrowUpRight :size="16" aria-hidden="true"
            /></a>
          </div>
        </div>
      </section>

      <section
        v-if="isAlternative"
        id="switching"
        class="comparison-section reading-section"
      >
        <h2>{{ copy('Plan the switch around one real workflow') }}</h2>
        <ol class="switching-steps">
          <li v-for="step in competitor.alternative.steps" :key="step">
            {{ copy(step) }}
          </li>
        </ol>
        <NuxtLinkLocale to="/docs/installation" class="comparison-link"
          >{{ copy('Read the installation guide') }}
          <ArrowRight :size="16" aria-hidden="true"
        /></NuxtLinkLocale>
        <div class="comparison-question">
          <h3>{{ copy(competitor.alternative.question) }}</h3>
          <p>{{ copy(competitor.alternative.answer) }}</p>
        </div>
      </section>
      <section v-else id="evaluate" class="comparison-section reading-section">
        <h2>{{ copy('Test the work, from report to reply') }}</h2>
        <p>{{ copy(competitor.versus.scenario) }}</p>
        <p>
          {{
            copy(
              'Use a test project and sample reports. Check mobile capture, teammate access, the destination issue and the final update before moving live feedback.',
            )
          }}
        </p>
        <NuxtLinkLocale
          :to="alternativePath(competitor.slug)"
          class="comparison-link"
          >{{ copy('Read the switching guide') }}
          <ArrowRight :size="16" aria-hidden="true"
        /></NuxtLinkLocale>
      </section>

      <section id="sources" class="comparison-section comparison-sources">
        <h2>{{ copy('Sources and review date') }}</h2>
        <p>
          {{
            copy(
              'Written by Fidibeki. Product information checked on {date}. This comparison uses published documentation, not a hands-on performance benchmark.',
              { date: reviewed },
            )
          }}
        </p>
        <ul>
          <li v-for="source in sources" :key="source.url">
            <a :href="source.url" rel="noopener" target="_blank"
              >{{ competitor.name }} · {{ copy(source.label) }}
              <ArrowUpRight :size="13" aria-hidden="true"
            /></a>
          </li>
          <li>
            <NuxtLinkLocale to="/features">{{
              copy('Fidibeki features')
            }}</NuxtLinkLocale>
          </li>
          <li>
            <NuxtLinkLocale to="/pricing">{{
              copy('Fidibeki pricing')
            }}</NuxtLinkLocale>
          </li>
        </ul>
        <NuxtLinkLocale to="/compare" class="comparison-link"
          >{{ copy('Explore the other comparisons') }}
          <ArrowRight :size="16" aria-hidden="true"
        /></NuxtLinkLocale>
      </section>
    </div>
    <CtaSection />
  </div>
</template>

<style scoped>
.comparison-body {
  padding-top: 12px;
}
.comparison-contents {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 28px;
  padding-block: 16px 28px;
  color: var(--muted);
  font-size: 14px;
}
.comparison-section {
  padding-block: 52px;
}
.comparison-section h2 {
  max-width: 760px;
  margin-bottom: 24px;
}
.comparison-section p {
  color: var(--muted);
}
.fit-section,
.price-comparison {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 64px;
}
.fit-section h2 {
  font-size: clamp(25px, 2.5vw, 32px);
}
.comparison-link {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
}
.comparison-link svg {
  flex-shrink: 0;
}
.section-intro {
  max-width: 680px;
  margin-bottom: 32px;
}
.comparison-table-wrap {
  overflow-x: auto;
}
.comparison-scroll-hint {
  display: none;
}
.comparison-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}
.comparison-table caption {
  text-align: left;
  color: var(--muted);
  font-size: 14px;
  padding-bottom: 20px;
}
.comparison-table th,
.comparison-table td {
  padding: 24px;
  text-align: left;
  vertical-align: top;
  border-bottom: 1px solid var(--line);
}
.comparison-table th {
  font-weight: 500;
}
.comparison-table thead {
  background: var(--surface-soft);
}
.comparison-table th:first-child {
  width: 24%;
  padding-left: 16px;
}
.comparison-table td {
  color: var(--muted);
}
.row-source {
  display: flex;
  align-items: center;
  gap: 5px;
  width: fit-content;
  color: var(--ink);
  font-size: 12px;
  margin-top: 10px;
}
.comparison-details {
  display: grid;
  gap: 36px;
  margin-top: 48px;
}
.comparison-details article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 64px;
}
.comparison-details p {
  max-width: 70ch;
  margin-bottom: 8px;
}
.price-comparison {
  background: var(--surface-soft);
  border-radius: 10px;
  padding: 32px;
}
.comparison-prices {
  list-style: none;
  padding: 0;
  margin: 24px 0;
  display: grid;
  gap: 20px;
}
.comparison-prices strong {
  display: block;
  font-size: 20px;
  font-weight: 500;
  line-height: 1.4;
}
.comparison-prices span {
  color: var(--muted);
  font-size: 13px;
}
.comparison-prices p {
  margin: 6px 0 0;
  font-size: 14px;
}
.reading-section {
  max-width: 760px;
}
.switching-steps {
  padding-left: 24px;
  display: grid;
  gap: 24px;
  color: var(--muted);
}
.switching-steps li {
  padding-left: 8px;
}
.comparison-question {
  margin-top: 48px;
}
.comparison-sources {
  padding-bottom: 24px;
}
.comparison-sources h2 {
  font-size: 25px;
}
.comparison-sources p {
  max-width: 760px;
  font-size: 14px;
}
.comparison-sources ul {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 24px;
  list-style: none;
  padding: 0;
  font-size: 14px;
}
@media (max-width: 899px) {
  .fit-section,
  .price-comparison {
    gap: 32px;
  }
  .comparison-details article {
    gap: 32px;
  }
}
@media (max-width: 650px) {
  .comparison-scroll-hint {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 13px;
    margin-bottom: 16px;
  }
  .comparison-scroll-hint svg {
    flex-shrink: 0;
  }
  .comparison-section {
    padding-block: 32px;
  }
  .fit-section,
  .price-comparison,
  .comparison-details article {
    grid-template-columns: minmax(0, 1fr);
  }
  .comparison-details article {
    gap: 8px;
  }
  .comparison-table {
    table-layout: auto;
    min-width: 650px;
  }
  .comparison-table th,
  .comparison-table td {
    padding: 18px 14px;
  }
  .price-comparison {
    padding: 24px 20px;
  }
}
</style>
