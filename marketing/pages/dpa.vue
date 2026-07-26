<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'

const activeId = ref('scope')
let observer: IntersectionObserver | null = null

const headings = [
  { id: 'scope', label: 'Scope and roles' },
  { id: 'instructions', label: 'Your instructions' },
  { id: 'duration', label: 'Duration' },
  { id: 'confidentiality', label: 'Confidentiality' },
  { id: 'security', label: 'Security' },
  { id: 'subprocessors', label: 'Sub-processors' },
  { id: 'transfers', label: 'Transfers' },
  { id: 'assistance', label: 'Data-subject rights' },
  { id: 'breach', label: 'Breach notification' },
  { id: 'dpia', label: 'DPIA assistance' },
  { id: 'deletion', label: 'Return and deletion' },
  { id: 'audit', label: 'Audits' },
  { id: 'ai', label: 'AI sub-processing' },
  { id: 'liability', label: 'Liability' },
  { id: 'annex-i', label: 'Annex I — Processing' },
  { id: 'annex-ii', label: 'Annex II — Measures' },
  { id: 'annex-iii', label: 'Annex III — Sub-processors' },
  { id: 'not-settled', label: 'Not yet settled' },
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
  title: 'Data Processing Addendum',
  description:
    'The Article 28 processor terms for feedback collected through your Fidibeki widget: instructions, security measures, sub-processors, breach notification, and an honest account of what is not yet settled.',
  path: '/dpa',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Data Processing Addendum', path: '/dpa' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      eyebrow="Trust"
      title="Data Processing Addendum"
      lede="When your users submit feedback through your widget, you are the controller and we are your processor. This sets out what we may do with that data, what we must do to protect it, and what we owe you when something goes wrong."
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[{ name: 'Home', path: '/' }, { name: 'Data Processing Addendum', path: '/dpa' }]"
        />
      </template>
    </PageHero>

    <section class="section legal-section">
      <div class="container legal-layout">
        <nav class="legal-nav" aria-label="Addendum sections">
          <div class="legal-nav-sticky">
            <ul class="legal-nav-list">
              <li v-for="heading in headings" :key="heading.id">
                <a :href="`#${heading.id}`" :class="{ active: activeId === heading.id }">{{
                  heading.label
                }}</a>
              </li>
            </ul>
          </div>
        </nav>

        <div class="prose legal-notice">
          <details class="effective-date">
            <summary>Effective date: 26 July 2026</summary>
            <p>
              This addendum forms part of the
              <NuxtLink to="/tos">terms of service</NuxtLink> and applies automatically to every
              customer processing personal data through Fidibeki. No signature is required, though
              we will sign a copy on request.
            </p>
          </details>

          <div class="review-banner">
            <strong>Read the last section first.</strong> Several provider-side facts this addendum
            would normally state — sub-processor locations, transfer mechanisms, and executed
            downstream agreements — are not yet verified. Rather than assert them, they are listed
            openly under <a href="#not-settled">what is not yet settled</a>.
          </div>

          <h2 id="scope">Scope and the role of each party</h2>
          <p>
            This addendum governs personal data that we process on your behalf. In practice that
            means feedback submitted through a widget you operate, together with anything a reporter
            chose to attach to it, and the derived records the service produces from that
            submission.
          </p>
          <p>
            <strong>You are the controller.</strong> You decide which product to put the widget on,
            which rules run, what is collected, and why. Establishing a lawful basis for that
            collection, and telling your users about it, is yours.
          </p>
          <p>
            <strong>We are the processor.</strong> We hold and process the data to run the service
            for you, on your instructions, and for nothing else.
          </p>
          <p>
            Some data is outside this split. Your own account, workspace configuration, and billing
            records are data for which we are the controller, described in the
            <NuxtLink to="/privacy">privacy notice</NuxtLink> rather than here. Where you are
            yourself a processor for someone else, you confirm you have the authority to appoint us
            as a sub-processor.
          </p>

          <h2 id="instructions">Processing on your documented instructions</h2>
          <p>
            We process customer personal data only on your documented instructions, including for
            international transfers, unless a law we are subject to requires otherwise. Where such a
            law applies, we will tell you before processing unless that law forbids it.
          </p>
          <p>Your instructions are, together:</p>
          <ul>
            <li>The terms of service and this addendum;</li>
            <li>
              Your configuration of the service — projects, widget rules, consent modes, retention
              window, integrations, and whether AI analysis is enabled for a project;
            </li>
            <li>Use of the product's documented features and API by your authorised members.</li>
          </ul>
          <p>
            If we believe an instruction infringes data-protection law, we will tell you and may
            pause the affected processing rather than carry it out.
          </p>

          <h2 id="duration">Subject matter and duration</h2>
          <p>
            The subject matter, nature, purpose, data categories, and categories of data subjects
            are set out in <a href="#annex-i">Annex I</a>. Processing lasts as long as your
            subscription, plus the deletion periods described under
            <a href="#deletion">return and deletion</a>.
          </p>

          <h2 id="confidentiality">Confidentiality</h2>
          <p>
            Access to customer personal data is limited to people who need it to run or support the
            service, and everyone with such access is bound by a duty of confidentiality that
            survives the end of their engagement.
          </p>
          <p>
            Support access to sensitive material — attachments, replays, exports — is logged with
            actor, timestamp, and purpose. That audit trail is a capability we are still completing;
            see <a href="#not-settled">what is not yet settled</a>.
          </p>

          <h2 id="security">Security of processing</h2>
          <p>
            We implement appropriate technical and organisational measures under Article 32,
            described concretely in <a href="#annex-ii">Annex II</a>. Those measures are the
            commitment, not the marketing summary: if we change them, they must remain at least
            equivalent in protection.
          </p>
          <p>
            We hold no SOC 2, ISO 27001, or comparable certification, and we do not imply otherwise
            anywhere in this document.
          </p>

          <h2 id="subprocessors">Sub-processors</h2>
          <p>
            You give general authorisation for us to engage sub-processors. Those engaged today, and
            what each one does, are listed in <a href="#annex-iii">Annex III</a>.
          </p>
          <p>
            Each sub-processor is engaged under a written contract imposing data-protection
            obligations no less protective than these, and we remain fully liable to you for their
            performance.
          </p>
          <p>
            <strong>Changes.</strong> We will give you at least 30 days' notice before a new
            sub-processor starts processing customer personal data. If you have a reasonable
            data-protection objection, tell us within that period and we will work to offer a
            change; if we cannot, you may terminate the affected part of the service and receive a
            pro-rata refund for the unused period. Silence for 30 days is acceptance.
          </p>

          <h2 id="transfers">International transfers</h2>
          <p>
            Where a sub-processor processes customer personal data outside the country you are in,
            including outside the European Economic Area, that transfer requires a valid mechanism —
            an adequacy decision, Standard Contractual Clauses, or another lawful route — together
            with a transfer impact assessment.
          </p>
          <p>
            We do not claim EU data residency for flows that do not support it, and we do not claim
            transfer mechanisms we have not confirmed. The per-provider position is recorded in
            <a href="#annex-iii">Annex III</a>, and where it is still unverified the annex says so
            rather than filling the gap.
          </p>

          <h2 id="assistance">Assisting you with data-subject requests</h2>
          <p>
            Reporters usually contact the company whose product they used, which is you. If a
            reporter contacts us directly about data we hold for you, we will not answer on your
            behalf; we will route the request to you and tell the reporter we have done so.
          </p>
          <p>
            Taking into account the nature of the processing, we assist you with appropriate
            technical and organisational measures in meeting requests for access, rectification,
            erasure, restriction, portability, and objection. The service provides self-serve export
            and deletion for feedback, attachments, and replays, which is normally enough to answer
            a request without our involvement. Where it is not, ask us.
          </p>

          <h2 id="breach">Personal data breach</h2>
          <p>
            We notify you without undue delay after becoming aware of a personal data breach
            affecting customer personal data, so that you can meet your own Article 33 deadline.
            Notification goes to the contact your workspace holds.
          </p>
          <p>The notification describes, as far as we know it at the time:</p>
          <ul>
            <li>The nature of the breach and the categories and approximate volume affected;</li>
            <li>The likely consequences;</li>
            <li>Measures taken or proposed, including to mitigate adverse effects;</li>
            <li>A contact point for further information.</li>
          </ul>
          <p>
            We keep an internal breach register recording incident facts, assessments, chronology,
            containment, and post-incident actions, held behind an administrative boundary separate
            from the tenant runtime. Notifications sent to you are preserved as frozen snapshots, so
            what you were told and when is reconstructable rather than a matter of memory.
          </p>
          <p>
            Notifying a supervisory authority or affected individuals is the controller's decision
            and remains yours. We will not make it for you.
          </p>

          <h2 id="dpia">Data protection impact assessments</h2>
          <p>
            We provide reasonable assistance with data protection impact assessments and prior
            consultation under Articles 35 and 36, limited to information about our processing that
            you cannot reasonably obtain yourself. The
            <NuxtLink to="/security">security page</NuxtLink>, this addendum, and its annexes are
            written to answer most of what an assessment asks.
          </p>
          <p>
            Session replay and AI analysis are the two capabilities most likely to require an
            assessment on your side. Both are off by default and become active only through a
            decision you make.
          </p>

          <h2 id="deletion">Return and deletion</h2>
          <p>
            During your subscription you can export and delete customer personal data yourself at
            any time. Replays and attachments are removed automatically once they pass your plan's
            retention window by a daily cleanup, and that deletion is permanent.
          </p>
          <p>
            On termination, deleting a workspace starts a seven-day grace period, after which the
            data is removed. For 30 days after termination we will help you export data on request.
            We delete existing copies unless a law we are subject to requires storage, in which case
            we tell you what is retained and why.
          </p>

          <h2 id="audit">Audits and information</h2>
          <p>
            We make available the information needed to demonstrate compliance with Article 28, and
            allow for and contribute to audits, including inspections, conducted by you or an
            auditor you mandate.
          </p>
          <p>
            In practice: written questionnaires and this documentation come first, and we will
            answer them properly. An on-site or hands-on audit is available where a supervisory
            authority requires it or after a breach affecting your data, on reasonable notice, no
            more than once a year unless the law demands otherwise, and subject to confidentiality
            and to not compromising other customers' data.
          </p>

          <h2 id="ai">AI sub-processing</h2>
          <p>
            AI analysis is disabled by default and is enabled per project by you. When it is on,
            free-text messages and the media a reporter chose to submit may be sent to Google Gemini
            for cleaning, transcription, sentiment scoring, and moderation flags.
          </p>
          <ul>
            <li><strong>Session replays are never sent to any AI provider.</strong></li>
            <li>
              Only the input categories explicitly enabled for the matching rule are sent, and
              authorisation is carried by the rule rather than assumed from the plan;
            </li>
            <li>
              Files placed with the provider target immediate deletion, tracked by a retry ledger;
            </li>
            <li>
              Our stored evidence of AI processing records the provider, model, purpose, category,
              and status — never the prompt, the source content, or raw provider output;
            </li>
            <li>Derived output follows the same retention, export, and deletion rules as its source.</li>
          </ul>
          <p>
            AI output is advisory. It can be wrong, and it should not be the sole basis for a
            decision producing legal or similarly significant effects for a person.
          </p>
          <p>
            The downstream contractual position with the AI provider is among the items under
            <a href="#not-settled">what is not yet settled</a>, and you should read that before
            enabling AI on a project carrying personal data.
          </p>

          <h2 id="liability">Liability and order of precedence</h2>
          <p>
            The liability limits in the <NuxtLink to="/tos">terms of service</NuxtLink> apply to
            this addendum, except where data-protection law does not permit them to. Nothing here
            limits a data subject's rights or a supervisory authority's powers.
          </p>
          <p>
            If this addendum conflicts with the terms of service on the processing of customer
            personal data, this addendum wins. A separately signed data processing agreement wins
            over both.
          </p>

          <h2 id="annex-i">Annex I — Description of the processing</h2>
          <h3>Subject matter and nature</h3>
          <p>
            Collecting product feedback through a widget you install, storing it, making it
            available to your authorised members for triage, and — where you enable them — analysing
            it, recording sessions, sending updates to reporters, and forwarding notifications to
            systems you connect.
          </p>
          <h3>Purpose</h3>
          <p>Providing the Fidibeki service to you, and supporting you in using it.</p>
          <h3>Categories of data subjects</h3>
          <ul>
            <li>Reporters — the users of your product who submit feedback;</li>
            <li>Your workspace members, in their capacity as users of the service;</li>
            <li>Any individual a reporter refers to in free text or captures in media.</li>
          </ul>
          <h3>Categories of personal data</h3>
          <ul>
            <li>Free-text feedback, ratings, and reactions;</li>
            <li>
              Attachments a reporter chose to add: screenshots, audio, video, element captures;
            </li>
            <li>
              Session replay, where you enabled it and the reporter consented — inputs masked,
              identity-bearing attributes stripped, canvas pixels suppressed;
            </li>
            <li>
              Reporter email address, only where the reporter opted in to updates about their
              feedback;
            </li>
            <li>
              Technical context: page origin, browser and device characteristics, and locally
              computed interaction signals;
            </li>
            <li>Workspace member identifiers, roles, and activity records.</li>
          </ul>
          <p>
            <strong>Special categories.</strong> Free text and media can contain health, political,
            religious, biometric, sexual, employment, or child-related information, because a
            reporter can type or capture anything. This is not data the service solicits, and
            configuring rules so that it is not solicited is your obligation under the terms.
          </p>
          <h3>Frequency and duration</h3>
          <p>
            Continuous for the duration of the subscription. Retention follows your plan's window
            for replays and attachments — 30, 90, or 365 days — with raw widget analytics events
            kept for 90 days and daily aggregates for two years.
          </p>

          <h2 id="annex-ii">Annex II — Technical and organisational measures</h2>
          <p>
            These are implemented today. Where something is still being built, it is marked as such
            here rather than listed as if it were finished.
          </p>
          <ul>
            <li>
              <strong>Tenant isolation.</strong> Application data is stored under enforced row-level
              security in PostgreSQL; media and replay objects use tenant-prefixed keys in private,
              S3-compatible storage;
            </li>
            <li>
              <strong>Authentication.</strong> Passwordless magic links, brief audience-scoped
              tokens, and rotating refresh cookies with reuse detection — token reuse outside a
              narrow window revokes the whole session family. SAML SSO is available, and a workspace
              can require it and disable local login;
            </li>
            <li>
              <strong>Authorisation.</strong> Explicit per-route permissions across projects, rules,
              feedback, issues, sessions, members, API keys, integrations, and account operations.
              Invitations can be restricted to verified company email domains;
            </li>
            <li>
              <strong>Upload pipeline.</strong> Attachments upload through brief presigned URLs,
              are verified for size and MIME type, and land quarantined until signature verification
              passes. Storage objects stay private; downloads and playback stream through the
              authenticated API rather than direct storage links;
            </li>
            <li>
              <strong>Encryption.</strong> Integration secrets and custom headers are encrypted at
              rest with AES-256-GCM and shown exactly once at creation. Outbound webhooks are signed
              with HMAC-SHA256. Transport is encrypted in transit;
            </li>
            <li>
              <strong>Abuse resistance.</strong> Public submission endpoints enforce per-IP and
              per-project rate ceilings, verify browser origin against your allowed domains, and
              apply daily AI cost budgets. Limiter identities are brief HMAC hashes, and diagnostics
              never persist raw network or visitor data;
            </li>
            <li>
              <strong>Capture minimisation.</strong> The widget never reads input values, private
              page text, or network payloads, and builds no visitor fingerprint. Durable widget
              storage exists only after purpose-specific consent;
            </li>
            <li>
              <strong>Telemetry scrubbing.</strong> Error events are filtered by a deterministic
              allowlist before leaving the application: request URLs, headers, cookies, bodies,
              user and IP data, breadcrumbs, and free-text exception messages are removed or
              replaced with fixed placeholders;
            </li>
            <li>
              <strong>Breach register.</strong> Incident facts, assessments, chronology, containment
              and post-incident actions are recorded append-only behind a platform-administration
              boundary excluded from the tenant runtime role;
            </li>
            <li>
              <strong>Deletion.</strong> A daily cleanup enforces plan retention windows for
              replays and attachments; workspace deletion runs on a seven-day grace period;
            </li>
            <li>
              <strong>Access logging — in progress.</strong> Logging of views and downloads of
              attachments, replays, and exports with actor, timestamp, and purpose is being
              completed. Treat it as a roadmap item, not a control you can rely on today.
            </li>
          </ul>

          <h2 id="annex-iii">Annex III — Sub-processors</h2>
          <p>
            Roles and purposes below are accurate. The location, legal entity, and transfer
            mechanism columns a complete annex would carry are
            <a href="#not-settled">not yet verified</a>, and are deliberately left unstated rather
            than guessed.
          </p>
          <ul>
            <li>
              <strong>Managed hosting, PostgreSQL, Redis, and S3-compatible object storage.</strong>
              Running the service and storing application data, media, and replay objects;
            </li>
            <li><strong>Stripe.</strong> Billing and subscription processing;</li>
            <li><strong>Resend.</strong> Transactional email delivery;</li>
            <li>
              <strong>Sentry.</strong> Error telemetry, receiving only allowlist-scrubbed events;
            </li>
            <li>
              <strong>Cloudflare Turnstile.</strong> Signup bot protection, receiving the challenge
              token and the submitting request's IP address;
            </li>
            <li>
              <strong>Google Gemini.</strong> AI analysis, only for projects where you enable it,
              and never for session replays;
            </li>
            <li>
              <strong>Slack, Microsoft Teams, and webhook endpoints you configure.</strong> Only
              where you connect them, and only the fields the destination's allowlist permits.
              These act on your instruction and are governed by your relationship with them.
            </li>
          </ul>
          <p>
            Umami analytics runs on the marketing site only, with visitor consent, and processes no
            customer personal data. It is listed in the
            <NuxtLink to="/privacy">privacy notice</NuxtLink> rather than here.
          </p>

          <h2 id="not-settled">What is not yet settled</h2>
          <p>
            A processor agreement is worth less than nothing if it asserts things nobody has
            checked. These items are open, and we would rather you knew that before relying on this
            document:
          </p>
          <ul>
            <li>
              <strong>Sub-processor locations, legal entities, and contract status.</strong> Not yet
              independently verified across providers. Until they are, Annex III states roles only;
            </li>
            <li>
              <strong>Transfer mechanisms.</strong> Which providers rely on adequacy, which on
              Standard Contractual Clauses, and the transfer impact assessment for each, are not yet
              documented per provider;
            </li>
            <li>
              <strong>The AI provider's downstream terms.</strong> The executed agreement, data
              retention and logging settings, and zero-data-retention status with the AI provider
              are not yet confirmed. If your feedback carries personal data you consider sensitive,
              leave AI analysis off for that project until this is closed;
            </li>
            <li>
              <strong>Maximum retention periods for several internal record types.</strong> Plan
              windows for replays and attachments are enforced in code; some audit, delivery,
              workflow, and evidence records have a bounded lifecycle that is implemented but no
              approved maximum period;
            </li>
            <li>
              <strong>Sensitive-category processing instructions.</strong> Project-level controls
              making AI use an explicit privacy decision rather than a plan default are still being
              built;
            </li>
            <li>
              <strong>Certification.</strong> No SOC 2, ISO 27001, or equivalent, and none claimed.
            </li>
          </ul>
          <p>
            We will update this addendum as these close, and tell you about material changes the
            same way we would any other change to the terms.
          </p>

          <h2 id="contact">Contact</h2>
          <p>
            For questions about this addendum, a signed copy, a security questionnaire, or a
            sub-processor objection, write to
            <a href="mailto:privacy@fidibeki.com">privacy@fidibeki.com</a>. Every contact route is
            on the <NuxtLink to="/contact">contact page</NuxtLink>.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      title="Reviewing us for a purchase?"
      body="Bring the questionnaire. We would rather answer the hard questions before you buy than after."
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

.review-banner {
  border-left: 3px solid var(--accent);
  padding: 0.85rem 1rem;
  margin-bottom: 2em;
  background: var(--surface);
  border-radius: 0 6px 6px 0;
  font-size: 0.92rem;
  color: var(--ink-soft);
  line-height: 1.6;
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
