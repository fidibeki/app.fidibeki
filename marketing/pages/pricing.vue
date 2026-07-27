<script setup lang="ts">
import { Check, Minus } from '@lucide/vue'
import {
  formatLimit,
  formatPrice,
  planComparisonRows,
  plans,
  trialDays,
} from '~/data/pricing'
import { siteName } from '~/data/site'

const links = useAppLinks()
const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')

useSiteMeta({
  title: 'Pricing',
  description: `Fidibeki plans from ${formatPrice(999)}/month with a ${trialDays}-day free trial and no credit card required. Compare Starter, Growth, Scale, and Enterprise.`,
  path: '/pricing',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Pricing', path: '/pricing' },
  ],
  structuredData: [
    {
      '@context': 'https://schema.org',
      '@type': 'SoftwareApplication',
      name: siteName,
      applicationCategory: 'BusinessApplication',
      operatingSystem: 'Web',
      url: `${siteUrl}/`,
      offers: plans
        .filter((plan) => plan.monthlyPriceCents !== null)
        .map((plan) => ({
          '@type': 'Offer',
          name: `${plan.name} plan`,
          price: (plan.monthlyPriceCents! / 100).toFixed(2),
          priceCurrency: 'USD',
          url: `${siteUrl}/pricing`,
        })),
    },
  ],
})

function planLimitLabel(plan: (typeof plans)[number]) {
  return {
    projects: formatLimit(plan.limits.projects, 'project'),
    feedbackPerMonth:
      plan.limits.feedbackPerMonth === -1
        ? 'Unlimited feedback'
        : `${plan.limits.feedbackPerMonth.toLocaleString('en-US')} feedback / month`,
    users: formatLimit(plan.limits.users, 'team member'),
    storageDays: plan.limits.storageDays === -1 ? 'Unlimited' : `${plan.limits.storageDays} days`,
  }
}

function comparisonValue(plan: (typeof plans)[number], key: (typeof planComparisonRows)[number]['key']) {
  if (key === 'projects') return formatLimit(plan.limits.projects, 'project')
  if (key === 'feedbackPerMonth')
    return plan.limits.feedbackPerMonth === -1
      ? 'Unlimited'
      : plan.limits.feedbackPerMonth.toLocaleString('en-US')
  if (key === 'users') return formatLimit(plan.limits.users, 'member')
  if (key === 'storageDays')
    return plan.limits.storageDays === -1 ? 'Unlimited' : `${plan.limits.storageDays} days`
  return plan.features[key]
}

const faq = [
  {
    question: 'How does the free trial work?',
    answer: `Every new workspace starts with a ${trialDays}-day free trial. No credit card is required to sign up. You pick a plan when the trial ends.`,
  },
  {
    question: 'What happens to replays and attachments over time?',
    answer:
      'Each plan includes a retention window for session replays and attachments: 30, 90, or 365 days. A daily cleanup removes data past your plan’s window.',
  },
  {
    question: 'Is yearly billing available?',
    answer:
      'Yes. Yearly billing discounts the monthly price: 7% on Starter and 14% on Growth and Scale.',
  },
  {
    question: 'How do I get Enterprise?',
    answer:
      'Enterprise is not self-serve. Book a demo and we will scope unlimited projects, feedback, members, retention, and a custom domain together.',
  },
]
</script>

<template>
  <div>
    <PageHero
      eyebrow="Pricing"
      :title="`Simple plans, ${trialDays}-day free trial`"
      lede="Start free, no credit card. Upgrade once the workflow proves its value. Prices shown are monthly; yearly billing is discounted."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Pricing', path: '/pricing' }]" />
      </template>
    </PageHero>

    <section class="section plans-section">
      <div class="container">
        <div class="plan-grid">
          <div
            v-for="plan in plans"
            :key="plan.id"
            class="card plan-card"
            :class="{ highlighted: plan.highlighted }"
          >
            <div class="plan-head">
              <h2>{{ plan.name }}</h2>
              <span v-if="plan.highlighted" class="badge">Most popular</span>
            </div>
            <p class="plan-blurb">{{ plan.blurb }}</p>
            <p class="plan-price">
              <span class="plan-amount">{{ formatPrice(plan.monthlyPriceCents) }}</span>
              <span v-if="plan.monthlyPriceCents !== null" class="plan-period">/month</span>
            </p>
            <p v-if="plan.yearlyDiscountPercent" class="plan-yearly">
              Save {{ plan.yearlyDiscountPercent }}% with yearly billing
            </p>
            <p v-else-if="!plan.selfServe" class="plan-yearly">Custom terms &amp; invoicing</p>
            <ul class="plan-limits">
              <li>{{ planLimitLabel(plan).projects }}</li>
              <li>{{ planLimitLabel(plan).feedbackPerMonth }}</li>
              <li>{{ planLimitLabel(plan).users }}</li>
              <li>{{ planLimitLabel(plan).storageDays === 'Unlimited' ? 'Unlimited' : `${planLimitLabel(plan).storageDays} of` }} stored replays &amp; attachments</li>
            </ul>
            <a
              v-if="plan.selfServe"
              :href="links.signUp"
              class="btn btn-large plan-cta"
              :class="plan.highlighted ? 'btn-primary' : 'btn-ghost'"
            >
              Start free trial
            </a>
            <a
              v-else
              :href="links.demo"
              class="btn btn-ghost btn-large plan-cta"
              rel="noopener"
              target="_blank"
            >
              Book a demo
            </a>
          </div>
        </div>
        <p class="plans-note">
          All prices in USD. Plan limits and feature availability are enforced by the product. What you see here is exactly what the billing system uses.
        </p>
      </div>
    </section>

    <section class="section comparison-section" aria-labelledby="comparison-heading">
      <div class="container">
        <div class="section-head">
          <h2 id="comparison-heading">Compare plans</h2>
        </div>
        <div class="table-scroll">
          <table class="comparison-table">
            <caption class="visually-hidden">Feature and limit comparison across Fidibeki plans</caption>
            <thead>
              <tr>
                <th scope="col">
                  <span class="visually-hidden">Capability</span>
                </th>
                <th v-for="plan in plans" :key="plan.id" scope="col">{{ plan.name }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in planComparisonRows" :key="row.key">
                <th scope="row">{{ row.label }}</th>
                <td v-for="plan in plans" :key="plan.id">
                  <template v-if="typeof comparisonValue(plan, row.key) === 'boolean'">
                    <Check v-if="comparisonValue(plan, row.key)" :size="17" aria-hidden="true" class="check-yes" />
                    <Minus v-else :size="17" aria-hidden="true" class="check-no" />
                    <span class="visually-hidden">{{ comparisonValue(plan, row.key) ? 'Included' : 'Not included' }}</span>
                  </template>
                  <template v-else>{{ comparisonValue(plan, row.key) }}</template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </section>

    <section class="section faq-section" aria-labelledby="faq-heading">
      <div class="container">
        <div class="section-head">
          <h2 id="faq-heading">Pricing questions</h2>
        </div>
        <dl class="faq-list">
          <div v-for="item in faq" :key="item.question" class="faq-item">
            <dt>{{ item.question }}</dt>
            <dd>{{ item.answer }}</dd>
          </div>
        </dl>
      </div>
    </section>

    <CtaSection />
  </div>
</template>

<style scoped>
.plan-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.plan-card {
  display: flex;
  flex-direction: column;
}

.plan-card.highlighted {
  border-color: var(--accent);
  box-shadow: var(--shadow-card);
}

.plan-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.plan-head h2 {
  margin: 0;
  font-size: 1.25rem;
}

.plan-blurb {
  margin: 0.5rem 0 1rem;
  color: var(--muted);
  font-size: 0.92rem;
  min-height: 2.6em;
}

.plan-price {
  margin: 0;
}

.plan-amount {
  font-family: var(--font-display);
  font-size: 2.1rem;
  font-weight: 640;
}

.plan-period {
  color: var(--muted);
}

.plan-yearly {
  margin: 0.25rem 0 0;
  color: var(--accent-text);
  font-size: 0.84rem;
  font-weight: 600;
}

.plan-limits {
  list-style: none;
  margin: 1.1rem 0 1.4rem;
  padding: 1.1rem 0 0;
  border-top: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
  font-size: 0.92rem;
  color: var(--ink-soft);
}

.plan-cta {
  margin-top: auto;
  width: 100%;
}

.plans-note {
  margin-top: 1.5rem;
  color: var(--muted);
  font-size: 0.88rem;
}

.comparison-section {
  padding-top: 0;
}

.table-scroll {
  /* Positioned so absolutely-placed .visually-hidden helpers inside the table
     are clipped by this scroller instead of escaping to the page. */
  position: relative;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
}

.comparison-table {
  width: 100%;
  min-width: 640px;
  border-collapse: collapse;
  background: var(--surface);
  font-size: 0.92rem;
}

.comparison-table th,
.comparison-table td {
  padding: 0.8rem 1rem;
  text-align: left;
  border-bottom: 1px solid var(--line);
}

.comparison-table thead th {
  font-family: var(--font-display);
  font-weight: 640;
  background: var(--surface-soft);
}

.comparison-table tbody th {
  font-weight: 600;
  color: var(--ink-soft);
}

.comparison-table tbody tr:last-child th,
.comparison-table tbody tr:last-child td {
  border-bottom: none;
}

.check-yes {
  color: var(--accent);
}

.check-no {
  color: var(--line-strong);
}

.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.faq-section {
  padding-top: 0;
}

.faq-list {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1.25rem;
  margin: 0;
}

.faq-item {
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: var(--surface);
  padding: 1.25rem 1.4rem;
}

.faq-item dt {
  font-family: var(--font-display);
  font-weight: 640;
  margin-bottom: 0.4rem;
}

.faq-item dd {
  margin: 0;
  color: var(--muted);
  font-size: 0.95rem;
}

@media (min-width: 760px) {
  .plan-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .faq-list {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (min-width: 1080px) {
  .plan-grid {
    grid-template-columns: repeat(4, 1fr);
  }
}
</style>
