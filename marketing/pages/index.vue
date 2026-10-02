<!--
THESIS: Let visitors give a little feedback before asking them to buy a feedback tool.
OWN-WORLD: Neutral grotesque, white space, charcoal controls, full photographic stories, restrained product examples.
STORY: Try a report, follow its resolution, discover conversations and context, then start a trial.
FIRST VIEWPORT: Quiet navigation above a modest centered question and a working composer. Example prompts sit below; the next story enters at the fold.
FORM: User-pinned OpenAI interaction-first homepage. Replaces the rejected headline/dashboard compositions. No seed selection; the reference is explicit.
-->
<script setup lang="ts">
const { copy } = useMarketingI18n()

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
useSiteMeta(() => ({
  title: 'Website feedback widget for SaaS teams',
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
          name: copy('{plan} plan', { plan: plan.name }),
          price: (plan.monthlyPriceCents! / 100).toFixed(2),
          priceCurrency: 'USD',
        })),
    },
  ],
}))
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
          <h2 id="stories-heading">{{ copy('A closer look at Fidibeki') }}</h2>
          <NuxtLinkLocale to="/features"
            >{{ copy('Explore the product') }}
            <ArrowRight :size="16" aria-hidden="true"
          /></NuxtLinkLocale>
        </div>
        <NuxtLinkLocale
          to="/features/website-conversations"
          class="home-conversation-story"
        >
          <img
            src="/images/home/conversation.jpg"
            :alt="copy('Two people talking across a table by a café window')"
            width="1600"
            height="1067"
            loading="lazy"
          />
          <div class="home-conversation-copy">
            <span>{{ copy('Website conversations') }}</span>
            <h3>
              <span>{{ copy('Meet your users.') }}</span
              ><span>{{ copy('Right where they are.') }}</span>
            </h3>
            <span class="home-story-action"
              >{{ copy('Start a conversation') }}
              <ArrowUpRight :size="19" aria-hidden="true"
            /></span>
          </div>
        </NuxtLinkLocale>
        <div class="home-story-caption">
          <p>
            {{
              copy(
                "See who is using your website. Ask what is getting in their way, turn a useful message into a tracked issue, and return to the conversation with an answer.",
              )
            }}
          </p>
          <NuxtLinkLocale
            to="/features/website-conversations"
            :aria-label="copy('Learn about website conversations')"
            ><ArrowUpRight :size="24" aria-hidden="true"
          /></NuxtLinkLocale>
        </div>

        <div class="home-editorial-pair">
          <article class="home-editorial">
            <NuxtLinkLocale
              to="/features/feedback-widget"
              class="home-context-art"
              :aria-label="copy('Explore feedback with screenshots and audio')"
            >
              <div class="home-report-example">
                <div class="home-example-head">
                  <BrandMark /><span>{{ copy('Your feedback') }}</span
                  ><span class="home-example-label">{{ copy('Example') }}</span>
                </div>
                <p class="home-example-report">
                  {{ copy('The button disappears') }}<br />{{
                    copy('on my phone.')
                  }}
                </p>
                <div class="home-example-attachments">
                  <span
                    ><Image :size="18" aria-hidden="true" />
                    {{ copy('screenshot.png') }}</span
                  ><span><Play :size="14" aria-hidden="true" /> 0:12</span>
                </div>
                <div class="home-example-bottom">
                  <span
                    ><Paperclip :size="15" aria-hidden="true" />
                    {{ copy('Context included') }}</span
                  ><span class="home-example-check"
                    ><Check :size="15" aria-hidden="true"
                  /></span>
                </div>
              </div>
            </NuxtLinkLocale>
            <div class="home-editorial-copy">
              <span>{{ copy('Feedback with context') }}</span>
              <h3>
                <NuxtLinkLocale to="/features/feedback-widget"
                  >{{ copy('Less back-and-forth.') }}<br />{{
                    copy('More to')
                  }}
                  <span class="home-keep-together"
                    >{{ copy('go on.') }}
                    <ArrowUpRight :size="21" aria-hidden="true" /></span
                ></NuxtLinkLocale>
              </h3>
              <p>
                {{
                  copy(
                    'A screenshot, a voice note, or a session replay can help explain what happened. Your users choose what to share.',
                  )
                }}
              </p>
            </div>
          </article>
          <article class="home-editorial">
            <NuxtLinkLocale
              to="/features/reporter-loop"
              class="home-resolution-art"
              :aria-label="copy('Explore updates to feedback reporters')"
            >
              <div class="home-resolution-message">
                <span
                  ><CheckCheck :size="22" aria-hidden="true" />
                  {{ copy('Resolved') }}</span
                >
                <p>{{ copy("It's fixed. Thanks for flagging it.") }}</p>
                <span class="home-resolution-from"
                  ><BrandMark /> {{ copy('Your team') }}
                  <span>{{ copy('Example update') }}</span></span
                >
              </div>
            </NuxtLinkLocale>
            <div class="home-editorial-copy">
              <span>{{ copy('The follow-through') }}</span>
              <h3>
                <NuxtLinkLocale to="/features/reporter-loop"
                  >{{ copy('They spoke up.') }}<br />{{
                    copy('Keep them in the')
                  }}
                  <span class="home-keep-together"
                    >{{ copy('loop.') }}
                    <ArrowUpRight :size="21" aria-hidden="true" /></span
                ></NuxtLinkLocale>
              </h3>
              <p>
                {{
                  copy(
                    'Turn feedback into tracked issues. When the work is done, send an update to the people who opted in.',
                  )
                }}
              </p>
            </div>
          </article>
        </div>
      </section>

      <section
        class="home-customer"
        :aria-label="copy('Customers using Fidibeki')"
      >
        <p>
          {{ copy('Helping teams listen.') }}<br />{{
            copy('Including the people behind')
          }}
        </p>
        <a
          v-for="customer in customers"
          :key="customer.name"
          :href="customer.url"
          target="_blank"
          rel="noopener"
          :aria-label="copy('Visit {name}', { name: customer.name })"
          ><img
            class="home-customer-light"
            :src="customer.logoLight"
            :alt="copy(customer.name)"
            width="176"
            height="48"
            loading="lazy" /><img
            class="home-customer-dark"
            :src="customer.logoDark"
            :alt="copy(customer.name)"
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
        <h2 id="testimonials-heading">
          {{ copy('From the teams using Fidibeki') }}
        </h2>
        <fidibeki-wall :wall="widgetTestimonialsKey" />
      </section>

      <section class="home-closing" aria-labelledby="closing-heading">
        <h2 id="closing-heading">
          {{ copy('Make it better.') }}<br />{{ copy('Start by listening.') }}
        </h2>
        <a :href="links.signUp" class="home-button"
          >{{ copy('Start free trial') }}
          <ArrowUpRight :size="17" aria-hidden="true"
        /></a>
        <p>
          {{ copy(trialDays) }} {{ copy('days free. No credit card required.')
          }}<br />{{ copy('Plans from') }}
          {{ copy(formatPrice(starter.monthlyPriceCents))
          }}{{ copy('/month.') }}
          <NuxtLinkLocale to="/pricing">{{
            copy('See pricing')
          }}</NuxtLinkLocale>
        </p>
      </section>
    </div>
  </div>
</template>
