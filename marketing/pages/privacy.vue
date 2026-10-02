<script setup lang="ts">
const { copy } = useMarketingI18n()

import { onMounted, onUnmounted, ref } from 'vue'

const activeId = ref('controller')
let observer: IntersectionObserver | null = null

const headings = [
  { id: 'controller', label: 'Controller' },
  { id: 'widget-collection', label: 'Widget collection' },
  { id: 'widget-consent', label: 'Widget consent' },
  { id: 'session-replay-consent', label: 'Replay consent' },
  { id: 'ai-processing', label: 'AI processing' },
  { id: 'what-we-collect', label: 'What we collect' },
  { id: 'how-we-use', label: 'How we use' },
  { id: 'legal-basis', label: 'Legal basis' },
  { id: 'subprocessors', label: 'Subprocessors' },
  { id: 'international-transfers', label: 'Transfers' },
  { id: 'cookies', label: 'Cookies' },
  { id: 'rights', label: 'Your rights' },
  { id: 'retention', label: 'Retention' },
  { id: 'children', label: 'Children' },
  { id: 'updates', label: 'Updates' },
  { id: 'contact', label: 'Contact' },
]

onMounted(() => {
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          activeId.value = entry.target.id
        }
      }
    },
    {
      rootMargin: '-20% 0px -70% 0px',
      threshold: 0,
    },
  )
  for (const heading of headings) {
    const element = document.getElementById(heading.id)
    if (element) observer.observe(element)
  }
})

onUnmounted(() => {
  observer?.disconnect()
})

useSiteMeta({
  title: 'Privacy',
  description:
    'How Fidibeki treats data: what the widget and service collect, why, how long it lives, who processes it, and the rights you have.',
  path: '/privacy',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Privacy', path: '/privacy' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      :eyebrow="copy('Trust')"
      :title="copy('Privacy')"
      :lede="
        copy(
          'A plain-language account of how Fidibeki treats data: what is collected, why, how long it lives, and who can get it out.',
        )
      "
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Privacy', path: '/privacy' },
          ]"
        />
      </template>
    </PageHero>

    <section class="section legal-section">
      <div class="container legal-layout">
        <nav class="legal-nav" :aria-label="copy('Privacy sections')">
          <div class="legal-nav-sticky">
            <ul class="legal-nav-list">
              <li v-for="heading in headings" :key="heading.id">
                <a
                  :href="`#${heading.id}`"
                  :class="{ active: activeId === heading.id }"
                  >{{ copy(heading.label) }}</a
                >
              </li>
            </ul>
          </div>
        </nav>

        <div class="prose legal-notice">
          <details class="effective-date">
            <summary>{{ copy('Effective date: 26 July 2026') }}</summary>
            <p>
              {{
                copy(
                  'This is the current version of the privacy notice. Previous versions will be archived here when the notice changes materially.',
                )
              }}
            </p>
          </details>

          <h2 id="controller">{{ copy('Who is the controller?') }}</h2>
          <p>
            {{
              copy(
                'Fidibeki is a service operated by ACIDINEY ALVARO CARVALHO SOARES DIAS, an independent service provider established in Portugal, who acts as the data controller for the Fidibeki website, accounts, and business administration data, except where Fidibeki processes personal data on behalf of its customers as a processor. Fidibeki is the name of the service, not a company or a separate legal entity. In this notice, “we”, “us”, and “our” mean that operator.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'For feedback collected through the widget on a customer’s product, the customer is the controller and we act as a processor under that customer’s instructions. If you submit feedback through a Fidibeki widget, the privacy notice of the company operating that site is the first place to look for information about your data.',
              )
            }}
          </p>

          <h2 id="widget-collection">
            {{ copy('What the widget collects and refuses to') }}
          </h2>
          <p>
            {{
              copy(
                'The widget collects only what a reporter deliberately submits: their message, optional rating or reaction, and any screenshots, audio, or session replay they choose to add. Targeting decisions are computed locally from interaction signals like active time and scroll depth. The widget never reads input values, private page text, or network payloads, and it never builds a personal fingerprint. Session replay, where enabled, masks inputs by default and is never sent to any AI provider.',
              )
            }}
          </p>

          <h2 id="widget-consent">
            {{ copy('Widget analytics and storage consent') }}
          </h2>
          <p>
            {{
              copy(
                'The widget loads only the code needed to render and respond to a feedback rule until the end user makes a privacy choice. Any durable storage — such as a pseudonymous visitor identifier, visit or scroll state, or funnel-event queue — is created only after the user consents to analytics or targeting for that specific purpose. Consent for analytics does not cover session replay, and neither covers reporter email updates. Fidibeki does not use these mechanisms to follow users across different customer sites.',
              )
            }}
          </p>

          <h2 id="session-replay-consent">
            {{ copy('Session replay and consent') }}
          </h2>
          <p>
            {{
              copy(
                'Session replay is disabled by default for every project and rule. When a rule enables replay, Fidibeki does not start rrweb capture, buffer events, create a session, or upload a DOM snapshot until the end user gives explicit, affirmative consent. We offer two consent models:',
              )
            }}
          </p>
          <ul>
            <li>
              <strong>{{ copy('Fidibeki-managed consent.') }}</strong>
              {{
                copy(
                  'We show a localized, accessible prompt on your behalf before capture begins. Accept and reject have equal prominence, and the user can withdraw permission as easily as it was granted.',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Customer-managed consent.') }}</strong>
              {{
                copy(
                  'The recorder stays dormant until your consent manager calls our runtime API with a structured grant that includes purpose, notice version, locale, source, and a consent reference. Revocation immediately stops capture, discards buffered events, and prevents new sessions.',
                )
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'Even after consent, replay stays anonymous: inputs are masked, identity-bearing attributes and unsafe URLs are stripped, canvas pixels are suppressed, and replay identifiers are isolated from other reporter data. We also reject unsafe rrweb events server side. Replays are never sent to any AI provider.',
              )
            }}
          </p>

          <h2 id="ai-processing">{{ copy('AI processing') }}</h2>
          <p>
            {{
              copy(
                "On eligible plans and only for projects where AI analysis is enabled, Fidibeki may transmit the reporter's free-text message and the attachments they chose to submit — screenshots, audio, video, or element captures — to Google Gemini for cleaning, transcription, sentiment scoring, and moderation flags. This happens only for submissions that enter the AI pipeline. Session replays are never sent to any AI provider.",
              )
            }}
          </p>
          <p>
            {{
              copy(
                'Free-text and media may contain highly personal categories such as health, political, religious, biometric, sexual, employment, or child-related information. We are building project-level processing instructions and sensitive-category handling so AI use is an explicit privacy decision rather than a plan default. Derived AI output participates in the same retention, export, and rights workflows as the original submission.',
              )
            }}
          </p>

          <h2 id="what-we-collect">
            {{ copy('What information we collect') }}
          </h2>
          <p>{{ copy('We collect information in three main ways:') }}</p>
          <ul>
            <li>
              <strong>{{ copy('Information you provide.') }}</strong>
              {{
                copy(
                  'When you sign up for a workspace, contact us, or subscribe to updates, you may give us your name, email address, workspace name, and billing details. Payment information is handled directly by Stripe and is not stored by Fidibeki.',
                )
              }}
            </li>
            <li>
              <strong>{{
                copy('Information from your device and usage.')
              }}</strong>
              {{
                copy(
                  'When you use our site or service, we collect technical data such as your IP address, browser type, operating system, referring URL, pages visited, and feature usage. We use this for security, abuse prevention, product improvement, and support.',
                )
              }}
            </li>
            <li>
              <strong>{{
                copy('Information from widget submissions.')
              }}</strong>
              {{
                copy(
                  'When a reporter uses a Fidibeki widget on a customer’s site, the customer decides what to collect. The widget only submits what the reporter deliberately provides, plus minimal technical context such as the page origin and interaction signals. Optional reporter email is collected only with a clear, specific opt-in for updates about that feedback.',
                )
              }}
            </li>
          </ul>

          <h2 id="how-we-use">{{ copy('How we use your information') }}</h2>
          <p>{{ copy('We use personal information to:') }}</p>
          <ul>
            <li>
              {{ copy('Provide, maintain, and improve the Fidibeki service;') }}
            </li>
            <li>
              {{
                copy(
                  'Manage your account and send service-related messages such as security alerts;',
                )
              }}
            </li>
            <li>{{ copy('Respond to questions and provide support;') }}</li>
            <li>
              {{
                copy(
                  'Detect, investigate, and prevent abuse, fraud, and security risks;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Communicate product news or offers where you have consented;',
                )
              }}
            </li>
            <li>
              {{
                copy('Comply with legal obligations and protect our rights.')
              }}
            </li>
          </ul>

          <h2 id="legal-basis">{{ copy('Legal basis for processing') }}</h2>
          <p>
            {{
              copy(
                'If you are in the European Economic Area or another region with similar laws, we process personal data only where we have a legal basis. The bases we rely on are:',
              )
            }}
          </p>
          <ul>
            <li>
              <strong>{{ copy('Performance of a contract') }}</strong
              >{{ copy('. To provide the service you signed up for;') }}
            </li>
            <li>
              <strong>{{ copy('Legitimate interests') }}</strong
              >{{
                copy(
                  '. For security, abuse prevention, and product improvement, balanced against your rights;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Consent') }}</strong
              >{{
                copy(
                  '. For optional marketing emails and for analytics cookies on this marketing site;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Legal obligation') }}</strong
              >{{
                copy('. Where the law requires us to process or retain data.')
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'You can withdraw consent at any time by using the unsubscribe link in emails or by declining analytics in the consent banner. Withdrawing consent does not affect the lawfulness of processing that happened before the withdrawal.',
              )
            }}
          </p>

          <h2 id="subprocessors">{{ copy('Subprocessors') }}</h2>
          <p>
            {{
              copy(
                'We use a limited set of subprocessors to run Fidibeki. Their roles are recorded in our',
              )
            }}
            <NuxtLinkLocale to="/dpa">{{
              copy('Data Processing Addendum')
            }}</NuxtLinkLocale
            >{{
              copy(
                ', which also states openly which provider-side facts — locations, legal entities, and transfer mechanisms — are not yet verified. The current list includes:',
              )
            }}
          </p>
          <ul>
            <li>
              <strong>{{ copy('Stripe') }}</strong
              >{{ copy('. Payment processing;') }}
            </li>
            <li>
              <strong>{{ copy('Resend') }}</strong
              >{{ copy('. Transactional email delivery;') }}
            </li>
            <li>
              <strong>{{ copy('Sentry') }}</strong
              >{{ copy('. Error and performance telemetry;') }}
            </li>
            <li>
              <strong>{{ copy('Cloudflare Turnstile') }}</strong
              >{{ copy('. Bot protection on signup;') }}
            </li>
            <li>
              <strong>{{ copy('Google Gemini') }}</strong
              >{{
                copy(
                  '. AI analysis, only when AI is enabled for a project, and never for session replays;',
                )
              }}
            </li>
            <li>
              <strong>{{
                copy(
                  'Managed hosting, database, queue, and object storage providers',
                )
              }}</strong
              >{{ copy('. Infrastructure and storage;') }}
            </li>
            <li>
              <strong>{{ copy('Umami') }}</strong
              >{{
                copy(
                  '. Privacy-friendly analytics on this marketing site, with your consent.',
                )
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'We do not sell personal information. We notify customers before adding or replacing a subprocessor that processes widget data, in line with the DPA.',
              )
            }}
          </p>

          <h2 id="international-transfers">
            {{ copy('International transfers') }}
          </h2>
          <p>
            {{
              copy(
                'Some subprocessors process data outside the country where you are located, including outside the European Economic Area. A transfer like that needs an approved safeguard — an adequacy decision, Standard Contractual Clauses, or another lawful route — together with a transfer impact assessment.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'We are still verifying which mechanism applies to each provider, and we would rather say so than imply coverage we have not confirmed. The',
              )
            }}
            <NuxtLinkLocale to="/dpa">{{
              copy('Data Processing Addendum')
            }}</NuxtLinkLocale>
            {{
              copy(
                'lists this among the items that are not yet settled. We do not claim EU data residency for flows that do not actually support it.',
              )
            }}
          </p>

          <h2 id="cookies">{{ copy('Cookies and tracking on this site') }}</h2>
          <p>
            {{
              copy(
                'This marketing site uses Umami analytics. Umami does not use cookies, does not collect personal data, and does not track you across sites. Analytics only loads after you accept it in the consent banner. You can change your choice at any time by clearing the consent choice in your browser for this site.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                "This site also runs our own Fidibeki feedback widget, the same product we sell, so we can collect feedback directly from visitors. Like analytics, it only loads after you accept in the consent banner, and any feedback you submit through it is processed the same way as feedback submitted through a customer's widget.",
              )
            }}
          </p>
          <p>
            {{
              copy(
                'The Fidibeki application may use essential session cookies and similar technologies to keep you signed in. Optional widget storage and analytics are gated behind purpose-specific consent where applicable.',
              )
            }}
          </p>

          <h2 id="rights">{{ copy('Your data protection rights') }}</h2>
          <p>
            {{
              copy('Depending on where you live, you may have the right to:')
            }}
          </p>
          <ul>
            <li>
              {{
                copy(
                  'Access, correct, update, or delete your personal information;',
                )
              }}
            </li>
            <li>{{ copy('Object to or restrict certain processing;') }}</li>
            <li>{{ copy('Request a portable copy of your data;') }}</li>
            <li>
              {{ copy('Withdraw consent for processing based on consent;') }}
            </li>
            <li>{{ copy('Complain to a supervisory authority.') }}</li>
          </ul>
          <p>
            {{
              copy(
                'Workspace members can manage their profile data in their account settings. Reporters who submitted feedback through a widget should contact the company that operates that site, or email us at',
              )
            }}
            <a href="mailto:privacy@fidibeki.com">{{
              copy('privacy@fidibeki.com')
            }}</a>
            {{
              copy('and we will route the request to the relevant customer.')
            }}
          </p>

          <h2 id="retention">{{ copy('Data retention') }}</h2>
          <p>
            {{
              copy(
                'We retain data only as long as needed for the purpose for which it was collected, or as required by law. Replays and attachments follow the plan storage window (30, 90, or 365 days). Raw widget analytics events are kept for 90 days; daily aggregates for two years. Workspace deletion starts a seven-day grace period, after which data is removed.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'We are completing a full retention register for authentication, delivery, audit, telemetry, and workflow data. Until it is published, contact us for current retention details.',
              )
            }}
          </p>

          <h2 id="children">{{ copy('Children') }}</h2>
          <p>
            {{
              copy(
                'Fidibeki is not intended for children under 16. The widget must not be used to collect personal data from children. If you believe a child has provided personal data through a widget without appropriate consent, contact the site operator or email us at',
              )
            }}
            <a href="mailto:privacy@fidibeki.com">{{
              copy('privacy@fidibeki.com')
            }}</a>
            {{ copy('and we will help route and remove it.') }}
          </p>

          <h2 id="updates">{{ copy('Updates to this privacy notice') }}</h2>
          <p>
            {{
              copy(
                'We update this notice as the product and legal context change. We will inform you of material changes through the service or by email where required by law. The effective date of the latest version is shown at the top of this page.',
              )
            }}
          </p>

          <h2 id="contact">{{ copy('How to contact us') }}</h2>
          <p>
            {{
              copy(
                'For privacy questions, data-rights requests, or DPA inquiries, email us at',
              )
            }}
            <a href="mailto:privacy@fidibeki.com">{{
              copy('privacy@fidibeki.com')
            }}</a
            >{{ copy('. General support is available at') }}
            <a href="mailto:support@fidibeki.com">{{
              copy('support@fidibeki.com')
            }}</a
            >{{
              copy(
                '. Every contact route, and the operator’s details, are on the',
              )
            }}
            <NuxtLinkLocale to="/contact">{{
              copy('contact page')
            }}</NuxtLinkLocale
            >.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      :title="copy('Questions about data handling?')"
      :body="
        copy(
          'Ask us directly. The people answering are the people who built the retention jobs.',
        )
      "
    />
  </div>
</template>
