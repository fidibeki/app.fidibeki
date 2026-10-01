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
    storageDays:
      plan.limits.storageDays === -1
        ? 'Unlimited'
        : `${plan.limits.storageDays} days`,
  }
}

function comparisonValue(
  plan: (typeof plans)[number],
  key: (typeof planComparisonRows)[number]['key'],
) {
  if (key === 'projects') return formatLimit(plan.limits.projects, 'project')
  if (key === 'feedbackPerMonth')
    return plan.limits.feedbackPerMonth === -1
      ? 'Unlimited'
      : plan.limits.feedbackPerMonth.toLocaleString('en-US')
  if (key === 'users') return formatLimit(plan.limits.users, 'member')
  if (key === 'storageDays')
    return plan.limits.storageDays === -1
      ? 'Unlimited'
      : `${plan.limits.storageDays} days`
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
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Pricing', path: '/pricing' },
          ]"
        />
      </template>
    </PageHero>

    <section class="section plans-section">
      <div class="container">
        <div class="plan-grid">
          <div
            v-for="plan in plans"
            :key="plan.id"
            class="plan-card"
            :class="{ highlighted: plan.highlighted }"
          >
            <div class="plan-head">
              <h2>{{ plan.name }}</h2>
              <span v-if="plan.highlighted" class="badge">Most popular</span>
            </div>
            <p class="plan-blurb">{{ plan.blurb }}</p>
            <p class="plan-price">
              <span class="plan-amount">{{
                formatPrice(plan.monthlyPriceCents)
              }}</span>
              <span v-if="plan.monthlyPriceCents !== null" class="plan-period"
                >/month</span
              >
            </p>
            <p v-if="plan.yearlyDiscountPercent" class="plan-yearly">
              Save {{ plan.yearlyDiscountPercent }}% with yearly billing
            </p>
            <p v-else-if="!plan.selfServe" class="plan-yearly">
              Custom terms &amp; invoicing
            </p>
            <ul class="plan-limits">
              <li>{{ planLimitLabel(plan).projects }}</li>
              <li>{{ planLimitLabel(plan).feedbackPerMonth }}</li>
              <li>{{ planLimitLabel(plan).users }}</li>
              <li>
                {{
                  planLimitLabel(plan).storageDays === 'Unlimited'
                    ? 'Unlimited'
                    : `${planLimitLabel(plan).storageDays} of`
                }}
                stored replays &amp; attachments
              </li>
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
          All prices in USD. Each plan includes a {{ trialDays }}-day free
          trial. No credit card required.
        </p>
      </div>
    </section>

    <section
      class="section comparison-section"
      aria-labelledby="comparison-heading"
    >
      <div class="container">
        <div class="section-head">
          <h2 id="comparison-heading">Compare plans</h2>
        </div>
        <div
          class="table-scroll"
          tabindex="0"
          role="region"
          aria-label="Plan comparison, scroll horizontally on smaller screens"
        >
          <table class="comparison-table">
            <caption class="visually-hidden">
              Feature and limit comparison across Fidibeki plans
            </caption>
            <thead>
              <tr>
                <th scope="col">
                  <span class="visually-hidden">Capability</span>
                </th>
                <th v-for="plan in plans" :key="plan.id" scope="col">
                  {{ plan.name }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in planComparisonRows" :key="row.key">
                <th scope="row">{{ row.label }}</th>
                <td v-for="plan in plans" :key="plan.id">
                  <template
                    v-if="typeof comparisonValue(plan, row.key) === 'boolean'"
                  >
                    <Check
                      v-if="comparisonValue(plan, row.key)"
                      :size="17"
                      aria-hidden="true"
                      class="check-yes"
                    />
                    <Minus
                      v-else
                      :size="17"
                      aria-hidden="true"
                      class="check-no"
                    />
                    <span class="visually-hidden">{{
                      comparisonValue(plan, row.key)
                        ? 'Included'
                        : 'Not included'
                    }}</span>
                  </template>
                  <template v-else>{{
                    comparisonValue(plan, row.key)
                  }}</template>
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
          <p>Trial, billing, and your data.</p>
        </div>
        <div class="faq-list">
          <details
            v-for="(item, index) in faq"
            :key="item.question"
            class="faq-item"
            name="pricing-questions"
            :open="index === 0"
          >
            <summary>
              <span>{{ item.question }}</span
              ><span class="faq-toggle" aria-hidden="true" />
            </summary>
            <div class="faq-answer">
              <p>{{ item.answer }}</p>
            </div>
          </details>
        </div>
      </div>
    </section>

    <CtaSection />
  </div>
</template>

<style scoped>
.plans-section {
  padding-top: 24px;
}
.plan-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}
.plan-card {
  display: flex;
  flex-direction: column;
  padding: 28px 20px;
  border-top: 1px solid var(--line);
}
.plan-card.highlighted {
  background: var(--surface-soft);
  border-top-color: transparent;
  border-radius: 10px;
}
.plan-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 28px;
}
.plan-head h2 {
  font-size: 23px;
  margin: 0;
}
.plan-head .badge {
  background: var(--canvas);
  font-size: 10px;
  white-space: nowrap;
}
.plan-blurb {
  margin: 16px 0 28px;
  color: var(--muted);
  font-size: 14px;
  min-height: 4.8em;
}
.plan-price {
  margin: 0;
  line-height: 1.2;
}
.plan-amount {
  font-size: 36px;
  font-weight: 500;
  letter-spacing: -0.03em;
}
.plan-period {
  font-size: 13px;
  color: var(--muted);
  margin-left: 3px;
}
.plan-yearly {
  font-size: 12px;
  color: var(--muted);
  margin: 12px 0 0;
}
.plan-limits {
  list-style: none;
  margin: 28px 0;
  padding: 24px 0 0;
  border-top: 1px solid var(--line);
  display: grid;
  gap: 12px;
  font-size: 14px;
  color: var(--ink-soft);
}
.plan-cta {
  margin-top: auto;
  width: 100%;
  padding-inline: 12px;
}
.plans-note {
  margin: 24px 20px 0;
  font-size: 12px;
  color: var(--muted);
}
.comparison-section {
  padding-top: 24px;
}
.table-scroll {
  position: relative;
  overflow-x: auto;
}
.comparison-table {
  width: 100%;
  min-width: 720px;
  border-collapse: collapse;
  font-size: 14px;
}
.comparison-table th,
.comparison-table td {
  padding: 20px 16px;
  text-align: left;
  border-bottom: 1px solid var(--line);
}
.comparison-table th:first-child {
  padding-left: 0;
  width: 32%;
}
.comparison-table thead th {
  font-weight: 500;
  font-size: 16px;
  border-bottom-color: var(--ink);
}
.comparison-table tbody th {
  font-weight: 400;
  color: var(--ink-soft);
}
.comparison-table td {
  width: 17%;
}
.comparison-table tbody tr:last-child th,
.comparison-table tbody tr:last-child td {
  border-bottom: 0;
}
.comparison-table tbody tr:hover {
  background: var(--surface-soft);
}
.check-yes {
  color: var(--ink);
}
.check-no {
  color: var(--muted);
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
.comparison-section {
  padding-bottom: 0;
}
.faq-section {
  padding-top: 56px;
}
.faq-section .container {
  display: grid;
  grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr);
  gap: 80px;
}
.faq-section .section-head {
  margin-bottom: 0;
  padding-top: 22px;
}
.faq-section h2 {
  font-size: 28px;
  margin-bottom: 16px;
}
.faq-section .section-head p {
  color: var(--muted);
  font-size: 15px;
  margin: 0;
}
.faq-item summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 28px;
  min-height: 74px;
  padding-block: 22px;
  cursor: pointer;
  list-style: none;
  font-size: 17px;
  font-weight: 500;
  line-height: 1.5;
}
.faq-item summary::-webkit-details-marker {
  display: none;
}
.faq-item summary::marker {
  content: '';
}
.faq-item summary:hover {
  color: var(--muted);
}
.faq-toggle {
  position: relative;
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}
.faq-toggle::before,
.faq-toggle::after {
  content: '';
  position: absolute;
  background: currentColor;
}
.faq-toggle::before {
  width: 14px;
  height: 1px;
  top: 7px;
  left: 1px;
}
.faq-toggle::after {
  width: 1px;
  height: 14px;
  left: 7px;
  top: 1px;
  transition: transform 180ms ease;
}
.faq-item[open] .faq-toggle::after {
  transform: scaleY(0);
}
.faq-answer {
  padding: 0 44px 24px 0;
}
.faq-answer p {
  margin: 0;
  color: var(--muted);
  font-size: 15px;
  line-height: 1.7;
}
@supports (interpolate-size: allow-keywords) {
  .faq-item {
    interpolate-size: allow-keywords;
  }
  .faq-item::details-content {
    height: 0;
    overflow: clip;
    transition:
      height 220ms ease,
      content-visibility 220ms;
    transition-behavior: allow-discrete;
  }
  .faq-item[open]::details-content {
    height: auto;
  }
}
@media (max-width: 1100px) {
  .plan-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 32px;
  }
  .plan-blurb {
    min-height: 3.2em;
  }
}
@media (max-width: 700px) {
  .faq-section .container {
    grid-template-columns: minmax(0, 1fr);
    gap: 28px;
  }
  .faq-section {
    padding-top: 40px;
  }
  .faq-section .section-head {
    padding-top: 0;
  }
  .faq-item summary {
    font-size: 16px;
    gap: 20px;
  }
  .faq-answer {
    padding-right: 28px;
  }
}
@media (max-width: 560px) {
  .plan-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 24px;
  }
  .plan-blurb {
    min-height: 0;
  }
  .plan-card {
    padding: 28px 20px;
  }
  .plans-note {
    margin-inline: 0;
  }
}
</style>
