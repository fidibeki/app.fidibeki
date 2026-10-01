<!--
THESIS: Let visitors give a little feedback before asking them to buy a feedback tool.
OWN-WORLD: Neutral grotesque, white space, charcoal controls, full photographic stories, restrained product examples.
STORY: Try a report, follow its resolution, discover conversations and context, then start a trial.
FIRST VIEWPORT: Quiet navigation above a modest centered question and a working composer. Example prompts sit below; the next story enters at the fold.
FORM: User-pinned OpenAI interaction-first homepage. Replaces the rejected headline/dashboard compositions. No seed selection; the reference is explicit.
-->
<script setup lang="ts">
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  CheckCheck,
  Image,
  Paperclip,
  Play,
} from '@lucide/vue'
import { formatPrice, plans, trialDays } from '~/data/pricing'
import { customers, siteDescription, siteName } from '~/data/site'

const links = useAppLinks()
const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')
const widgetTestimonialsKey = config.public.widgetTestimonialsKey
const starter = plans.find((plan) => plan.id === 'starter')!
useSiteMeta({
  title: 'Product feedback that finishes the job',
  description: siteDescription,
  path: '/',
  structuredData: [
    {
      '@context': 'https://schema.org',
      '@type': 'SoftwareApplication',
      name: siteName,
      applicationCategory: 'BusinessApplication',
      operatingSystem: 'Web',
      url: `${siteUrl}/`,
      description: siteDescription,
      offers: plans
        .filter((plan) => plan.monthlyPriceCents !== null)
        .map((plan) => ({
          '@type': 'Offer',
          name: `${plan.name} plan`,
          price: (plan.monthlyPriceCents! / 100).toFixed(2),
          priceCurrency: 'USD',
        })),
    },
  ],
})
if (widgetTestimonialsKey) {
  useHead({
    script: [
      {
        src: 'https://widget.fidibeki.com/v1/testimonials.js',
        async: true,
        defer: true,
      },
    ],
  })
}
</script>

<template>
  <div class="home-page">
    <HomeFeedbackDemo />

    <div class="home-width">
      <section class="home-stories" aria-labelledby="stories-heading">
        <div class="home-section-heading">
          <h2 id="stories-heading">A closer look at Fidibeki</h2>
          <NuxtLink to="/features"
            >Explore the product <ArrowRight :size="16" aria-hidden="true"
          /></NuxtLink>
        </div>
        <NuxtLink
          to="/use-cases/customer-support"
          class="home-conversation-story"
        >
          <img
            src="/images/home/conversation.jpg"
            alt="Two people talking across a table by a café window"
            width="1600"
            height="1067"
            loading="lazy"
          />
          <div class="home-conversation-copy">
            <span>Website conversations</span>
            <h3>
              <span>Meet your users.</span><span>Right where they are.</span>
            </h3>
            <span class="home-story-action"
              >Start a conversation <ArrowUpRight :size="19" aria-hidden="true"
            /></span>
          </div>
        </NuxtLink>
        <div class="home-story-caption">
          <p>
            See who's on your website. Offer help, ask a question, or let
            visitors message your team. All through the same widget they use for
            feedback.
          </p>
          <NuxtLink
            to="/use-cases/customer-support"
            aria-label="Learn about website conversations"
            ><ArrowUpRight :size="24" aria-hidden="true"
          /></NuxtLink>
        </div>

        <div class="home-editorial-pair">
          <article class="home-editorial">
            <NuxtLink
              to="/features/feedback-widget"
              class="home-context-art"
              aria-label="Explore feedback with screenshots and audio"
            >
              <div class="home-report-example">
                <div class="home-example-head">
                  <BrandMark /><span>Your feedback</span
                  ><span class="home-example-label">Example</span>
                </div>
                <p class="home-example-report">
                  The button disappears<br />on my phone.
                </p>
                <div class="home-example-attachments">
                  <span
                    ><Image :size="18" aria-hidden="true" />
                    screenshot.png</span
                  ><span><Play :size="14" aria-hidden="true" /> 0:12</span>
                </div>
                <div class="home-example-bottom">
                  <span
                    ><Paperclip :size="15" aria-hidden="true" /> Context
                    included</span
                  ><span class="home-example-check"
                    ><Check :size="15" aria-hidden="true"
                  /></span>
                </div>
              </div>
            </NuxtLink>
            <div class="home-editorial-copy">
              <span>Feedback with context</span>
              <h3>
                <NuxtLink to="/features/feedback-widget"
                  >Less back-and-forth.<br />More to
                  <span class="home-keep-together"
                    >go on. <ArrowUpRight :size="21" aria-hidden="true" /></span
                ></NuxtLink>
              </h3>
              <p>
                A screenshot, a voice note, or a session replay can help explain
                what happened. Your users choose what to share.
              </p>
            </div>
          </article>
          <article class="home-editorial">
            <NuxtLink
              to="/features/reporter-loop"
              class="home-resolution-art"
              aria-label="Explore updates to feedback reporters"
            >
              <div class="home-resolution-message">
                <span
                  ><CheckCheck :size="22" aria-hidden="true" /> Resolved</span
                >
                <p>It's fixed.<br />Thanks for<br />flagging it.</p>
                <span class="home-resolution-from"
                  ><BrandMark /> Your team <span>Example update</span></span
                >
              </div>
            </NuxtLink>
            <div class="home-editorial-copy">
              <span>The follow-through</span>
              <h3>
                <NuxtLink to="/features/reporter-loop"
                  >They spoke up.<br />Keep them in the
                  <span class="home-keep-together"
                    >loop. <ArrowUpRight :size="21" aria-hidden="true" /></span
                ></NuxtLink>
              </h3>
              <p>
                Turn feedback into tracked issues. When the work is done, send
                an update to the people who opted in.
              </p>
            </div>
          </article>
        </div>
      </section>

      <section class="home-customer" aria-label="Customers using Fidibeki">
        <p>Helping teams listen.<br />Including the people behind</p>
        <a
          v-for="customer in customers"
          :key="customer.name"
          :href="customer.url"
          target="_blank"
          rel="noopener"
          :aria-label="`Visit ${customer.name}`"
          ><img
            class="home-customer-light"
            :src="customer.logoLight"
            :alt="customer.name"
            width="176"
            height="48"
            loading="lazy" /><img
            class="home-customer-dark"
            :src="customer.logoDark"
            :alt="customer.name"
            width="176"
            height="48"
            loading="lazy" /><ArrowUpRight :size="19" aria-hidden="true"
        /></a>
      </section>

      <HomeIntegrations />
      <section
        v-if="widgetTestimonialsKey"
        class="home-testimonials"
        aria-labelledby="testimonials-heading"
      >
        <h2 id="testimonials-heading">From the teams using Fidibeki</h2>
        <fidibeki-wall :wall="widgetTestimonialsKey" />
      </section>

      <section class="home-closing" aria-labelledby="closing-heading">
        <h2 id="closing-heading">Make it better.<br />Start by listening.</h2>
        <a :href="links.signUp" class="home-button"
          >Start free trial <ArrowUpRight :size="17" aria-hidden="true"
        /></a>
        <p>
          {{ trialDays }} days free. No credit card required.<br />Plans from
          {{ formatPrice(starter.monthlyPriceCents) }}/month.
          <NuxtLink to="/pricing">See pricing</NuxtLink>
        </p>
      </section>
    </div>
  </div>
</template>
