<script setup lang="ts">
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
    }
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
      eyebrow="Trust"
      title="Privacy"
      lede="A plain-language account of how Fidibeki treats data: what is collected, why, how long it lives, and who can get it out."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Privacy', path: '/privacy' }]" />
      </template>
    </PageHero>

    <section class="section legal-section">
      <div class="container legal-layout">
        <nav class="legal-nav" aria-label="Privacy sections">
          <div class="legal-nav-sticky">
            <ul class="legal-nav-list">
              <li v-for="heading in headings" :key="heading.id">
                <a :href="`#${heading.id}`" :class="{ active: activeId === heading.id }">{{ heading.label }}</a>
              </li>
            </ul>
          </div>
        </nav>

        <div class="prose legal-notice">
          <details class="effective-date">
            <summary>Effective date: 23 July 2026</summary>
            <p>
              This is the current version of the privacy notice. Previous versions will be
              archived here when the notice changes materially.
            </p>
          </details>

          <h2 id="controller">Who is the controller?</h2>
          <p>
            Fidibeki is a service operated by ACIDINEY ALVARO CARVALHO SOARES DIAS, an independent
            service provider established in Portugal, who acts as the data controller for the
            Fidibeki website, accounts, and business administration data, except where Fidibeki
            processes personal data on behalf of its customers as a processor. Fidibeki is the name
            of the service, not a company or a separate legal entity. In this notice, “we”, “us”,
            and “our” mean that operator.
          </p>
          <p>
            For feedback collected through the widget on a customer’s product, the customer is the
            controller and we act as a processor under that customer’s instructions. If you submit
            feedback through a Fidibeki widget, the privacy notice of the company operating that
            site is the first place to look for information about your data.
          </p>

          <h2 id="widget-collection">What the widget collects and refuses to</h2>
          <p>
            The widget collects only what a reporter deliberately submits: their message, optional
            rating or reaction, and any screenshots, audio, or session replay they choose to add.
            Targeting decisions are computed locally from interaction signals like active time and
            scroll depth. The widget never reads input values, private page text, or network
            payloads, and it never builds a personal fingerprint. Session replay, where enabled,
            masks inputs by default and is never sent to any AI provider.
          </p>

          <h2 id="widget-consent">Widget analytics and storage consent</h2>
          <p>
            The widget loads only the code needed to render and respond to a feedback rule until the
            end user makes a privacy choice. Any durable storage — such as a pseudonymous visitor
            identifier, visit or scroll state, or funnel-event queue — is created only after the user
            consents to analytics or targeting for that specific purpose. Consent for analytics does
            not cover session replay, and neither covers reporter email updates. Fidibeki does not use
            these mechanisms to follow users across different customer sites.
          </p>

          <h2 id="session-replay-consent">Session replay and consent</h2>
          <p>
            Session replay is disabled by default for every project and rule. When a rule enables
            replay, Fidibeki does not start rrweb capture, buffer events, create a session, or upload
            a DOM snapshot until the end user gives explicit, affirmative consent. We offer two
            consent models:
          </p>
          <ul>
            <li>
              <strong>Fidibeki-managed consent.</strong> We show a localized, accessible prompt on
              your behalf before capture begins. Accept and reject have equal prominence, and the
              user can withdraw permission as easily as it was granted.
            </li>
            <li>
              <strong>Customer-managed consent.</strong> The recorder stays dormant until your consent
              manager calls our runtime API with a structured grant that includes purpose, notice
              version, locale, source, and a consent reference. Revocation immediately stops capture,
              discards buffered events, and prevents new sessions.
            </li>
          </ul>
          <p>
            Even after consent, replay stays anonymous: inputs are masked, identity-bearing
            attributes and unsafe URLs are stripped, canvas pixels are suppressed, and replay
            identifiers are isolated from other reporter data. We also reject unsafe rrweb events
            server side. Replays are never sent to any AI provider.
          </p>

          <h2 id="ai-processing">AI processing</h2>
          <p>
            On eligible plans and only for projects where AI analysis is enabled, Fidibeki may transmit
            the reporter's free-text message and the attachments they chose to submit — screenshots,
            audio, video, or element captures — to Google Gemini for cleaning, transcription,
            sentiment scoring, and moderation flags. This happens only for submissions that enter
            the AI pipeline. Session replays are never sent to any AI provider.
          </p>
          <p>
            Free-text and media may contain highly personal categories such as health, political,
            religious, biometric, sexual, employment, or child-related information. We are building
            project-level processing instructions and sensitive-category handling so AI use is an
            explicit privacy decision rather than a plan default. Derived AI output participates
            in the same retention, export, and rights workflows as the original submission.
          </p>

          <h2 id="what-we-collect">What information we collect</h2>
          <p>We collect information in three main ways:</p>
          <ul>
            <li>
              <strong>Information you provide.</strong> When you sign up for a workspace, contact
              us, or subscribe to updates, you may give us your name, email address, workspace name,
              and billing details. Payment information is handled directly by Stripe and is not
              stored by Fidibeki.
            </li>
            <li>
              <strong>Information from your device and usage.</strong> When you use our site or
              service, we collect technical data such as your IP address, browser type, operating
              system, referring URL, pages visited, and feature usage. We use this for security,
              abuse prevention, product improvement, and support.
            </li>
            <li>
              <strong>Information from widget submissions.</strong> When a reporter uses a Fidibeki
              widget on a customer’s site, the customer decides what to collect. The widget only
              submits what the reporter deliberately provides, plus minimal technical context such
              as the page origin and interaction signals. Optional reporter email is collected only
              with a clear, specific opt-in for updates about that feedback.
            </li>
          </ul>

          <h2 id="how-we-use">How we use your information</h2>
          <p>We use personal information to:</p>
          <ul>
            <li>Provide, maintain, and improve the Fidibeki service;</li>
            <li>Manage your account and send service-related messages such as security alerts;</li>
            <li>Respond to questions and provide support;</li>
            <li>Detect, investigate, and prevent abuse, fraud, and security risks;</li>
            <li>Communicate product news or offers where you have consented;</li>
            <li>Comply with legal obligations and protect our rights.</li>
          </ul>

          <h2 id="legal-basis">Legal basis for processing</h2>
          <p>
            If you are in the European Economic Area or another region with similar laws, we process
            personal data only where we have a legal basis. The bases we rely on are:
          </p>
          <ul>
            <li>
              <strong>Performance of a contract</strong>. To provide the service you signed up
              for;
            </li>
            <li>
              <strong>Legitimate interests</strong>. For security, abuse prevention, and product
              improvement, balanced against your rights;
            </li>
            <li>
              <strong>Consent</strong>. For optional marketing emails and for analytics cookies
              on this marketing site;
            </li>
            <li>
              <strong>Legal obligation</strong>. Where the law requires us to process or retain
              data.
            </li>
          </ul>
          <p>
            You can withdraw consent at any time by using the unsubscribe link in emails or by
            declining analytics in the consent banner. Withdrawing consent does not affect the
            lawfulness of processing that happened before the withdrawal.
          </p>

          <h2 id="subprocessors">Subprocessors</h2>
          <p>
            We use a limited set of subprocessors to run Fidibeki. Their roles are recorded in our
            <NuxtLink to="/dpa">Data Processing Addendum</NuxtLink>, which also states openly which
            provider-side facts — locations, legal entities, and transfer mechanisms — are not yet
            verified. The current list includes:
          </p>
          <ul>
            <li><strong>Stripe</strong>. Payment processing;</li>
            <li><strong>Resend</strong>. Transactional email delivery;</li>
            <li><strong>Sentry</strong>. Error and performance telemetry;</li>
            <li><strong>Cloudflare Turnstile</strong>. Bot protection on signup;</li>
            <li>
              <strong>Google Gemini</strong>. AI analysis, only when AI is enabled for a project,
              and never for session replays;
            </li>
            <li>
              <strong>Managed hosting, database, queue, and object storage providers</strong>.
              Infrastructure and storage;
            </li>
            <li>
              <strong>Umami</strong>. Privacy-friendly analytics on this marketing site, with
              your consent.
            </li>
          </ul>
          <p>
            We do not sell personal information. We notify customers before adding or replacing a
            subprocessor that processes widget data, in line with the DPA.
          </p>

          <h2 id="international-transfers">International transfers</h2>
          <p>
            Some subprocessors process data outside the country where you are located, including
            outside the European Economic Area. A transfer like that needs an approved safeguard —
            an adequacy decision, Standard Contractual Clauses, or another lawful route — together
            with a transfer impact assessment.
          </p>
          <p>
            We are still verifying which mechanism applies to each provider, and we would rather say
            so than imply coverage we have not confirmed. The
            <NuxtLink to="/dpa">Data Processing Addendum</NuxtLink> lists this among the items that
            are not yet settled. We do not claim EU data residency for flows that do not actually
            support it.
          </p>

          <h2 id="cookies">Cookies and tracking on this site</h2>
          <p>
            This marketing site uses Umami analytics. Umami does not use cookies, does not collect
            personal data, and does not track you across sites. Analytics only loads after you accept
            it in the consent banner. You can change your choice at any time by clearing the
            consent choice in your browser for this site.
          </p>
          <p>
            This site also runs our own Fidibeki feedback widget, the same product we sell, so we can
            collect feedback directly from visitors. Like analytics, it only loads after you accept in
            the consent banner, and any feedback you submit through it is processed the same way as
            feedback submitted through a customer's widget.
          </p>
          <p>
            The Fidibeki application may use essential session cookies and similar technologies to
            keep you signed in. Optional widget storage and analytics are gated behind
            purpose-specific consent where applicable.
          </p>

          <h2 id="rights">Your data protection rights</h2>
          <p>Depending on where you live, you may have the right to:</p>
          <ul>
            <li>Access, correct, update, or delete your personal information;</li>
            <li>Object to or restrict certain processing;</li>
            <li>Request a portable copy of your data;</li>
            <li>Withdraw consent for processing based on consent;</li>
            <li>Complain to a supervisory authority.</li>
          </ul>
          <p>
            Workspace members can manage their profile data in their account settings. Reporters who
            submitted feedback through a widget should contact the company that operates that site,
            or email us at <a href="mailto:privacy@fidibeki.com">privacy@fidibeki.com</a> and we
            will route the request to the relevant customer.
          </p>

          <h2 id="retention">Data retention</h2>
          <p>
            We retain data only as long as needed for the purpose for which it was collected, or as
            required by law. Replays and attachments follow the plan storage window (30, 90, or 365
            days). Raw widget analytics events are kept for 90 days; daily aggregates for two
            years. Workspace deletion starts a seven-day grace period, after which data is removed.
          </p>
          <p>
            We are completing a full retention register for authentication, delivery, audit,
            telemetry, and workflow data. Until it is published, contact us for current retention
            details.
          </p>

          <h2 id="children">Children</h2>
          <p>
            Fidibeki is not intended for children under 16. The widget must not be used to
            collect personal data from children. If you believe a child has provided personal data
            through a widget without appropriate consent, contact the site operator or email us at
            <a href="mailto:privacy@fidibeki.com">privacy@fidibeki.com</a> and we will help
            route and remove it.
          </p>

          <h2 id="updates">Updates to this privacy notice</h2>
          <p>
            We update this notice as the product and legal context change. We will inform you of
            material changes through the service or by email where required by law. The effective
            date of the latest version is shown at the top of this page.
          </p>

          <h2 id="contact">How to contact us</h2>
          <p>
            For privacy questions, data-rights requests, or DPA inquiries, email us at
            <a href="mailto:privacy@fidibeki.com">privacy@fidibeki.com</a>. General support is
            available at <a href="mailto:support@fidibeki.com">support@fidibeki.com</a>. Every
            contact route, and the operator’s details, are on the
            <NuxtLink to="/contact">contact page</NuxtLink>.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      title="Questions about data handling?"
      body="Ask us directly. The people answering are the people who built the retention jobs."
    />
  </div>
</template>

<style scoped>
.legal-section {
  background: var(--surface-soft);
}

.legal-layout {
  display: grid;
  gap: 2rem;
}

.legal-nav {
  order: -1;
}

.legal-nav-sticky {
  position: sticky;
  top: calc(var(--header-height, 72px) + 1.5rem);
}

.legal-nav-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.legal-nav-list a {
  display: block;
  padding: 0.35rem 0.65rem;
  border-radius: 999px;
  background: var(--surface);
  border: 1px solid var(--line);
  color: var(--ink-soft);
  font-size: 0.85rem;
  text-decoration: none;
  white-space: nowrap;
}

.legal-nav-list a:hover {
  border-color: var(--accent);
  color: var(--accent-text);
}

.legal-notice {
  padding-top: 0.25rem;
}

.effective-date {
  font-size: 0.85rem;
  color: var(--muted);
  margin-bottom: 1.8em;
}

.effective-date summary {
  cursor: pointer;
  list-style: none;
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  font-weight: 500;
}

.effective-date summary::marker,
.effective-date summary::-webkit-details-marker {
  display: none;
}

.effective-date summary::after {
  content: '›';
  display: inline-block;
  transform: rotate(90deg);
  transition: transform 160ms ease;
  color: var(--accent-text);
}

.effective-date[open] summary::after {
  transform: rotate(-90deg);
}

.effective-date p {
  margin-top: 0.6rem;
  max-width: 44rem;
}

.legal-notice h2 {
  font-size: 1.35rem;
  margin-top: 2.2em;
}

.legal-notice h3 {
  font-size: 1.1rem;
  margin-top: 1.6em;
  color: var(--ink-soft);
}

.legal-notice p,
.legal-notice ul {
  color: var(--ink-soft);
}

.legal-notice li {
  margin-bottom: 0.45em;
}

.legal-nav-list a.active {
  color: var(--accent-text);
  font-weight: 600;
}

@media (min-width: 900px) {
  .legal-layout {
    grid-template-columns: 14rem 1fr;
    gap: 3rem;
  }

  .legal-nav {
    order: 0;
  }

  .legal-nav-list {
    flex-direction: column;
    gap: 0.15rem;
  }

  .legal-nav-list a {
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 0.35rem 0;
    white-space: normal;
  }

  .legal-nav-list a.active {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }

  .legal-nav-list a:hover {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }
}
</style>
