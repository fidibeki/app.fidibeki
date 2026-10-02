<script setup lang="ts">
const { copy } = useMarketingI18n()

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
      :eyebrow="copy('Trust')"
      :title="copy('Data Processing Addendum')"
      :lede="
        copy(
          'When your users submit feedback through your widget, you are the controller and we are your processor. This sets out what we may do with that data, what we must do to protect it, and what we owe you when something goes wrong.',
        )
      "
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Data Processing Addendum', path: '/dpa' },
          ]"
        />
      </template>
    </PageHero>

    <section class="section legal-section">
      <div class="container legal-layout">
        <nav class="legal-nav" :aria-label="copy('Addendum sections')">
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
              {{ copy('This addendum forms part of the') }}
              <NuxtLinkLocale to="/tos">{{
                copy('terms of service')
              }}</NuxtLinkLocale>
              {{
                copy(
                  'and applies automatically to every customer processing personal data through Fidibeki. No signature is required, though we will sign a copy on request.',
                )
              }}
            </p>
          </details>

          <div class="review-banner">
            <strong>{{ copy('Read the last section first.') }}</strong>
            {{
              copy(
                'Several provider-side facts this addendum would normally state — sub-processor locations, transfer mechanisms, and executed downstream agreements — are not yet verified. Rather than assert them, they are listed openly under',
              )
            }}
            <a href="#not-settled">{{ copy('what is not yet settled') }}</a
            >.
          </div>

          <h2 id="scope">{{ copy('Scope and the role of each party') }}</h2>
          <p>
            {{
              copy(
                'This addendum governs personal data that we process on your behalf. In practice that means feedback submitted through a widget you operate, together with anything a reporter chose to attach to it, and the derived records the service produces from that submission.',
              )
            }}
          </p>
          <p>
            <strong>{{ copy('You are the controller.') }}</strong>
            {{
              copy(
                'You decide which product to put the widget on, which rules run, what is collected, and why. Establishing a lawful basis for that collection, and telling your users about it, is yours.',
              )
            }}
          </p>
          <p>
            <strong>{{ copy('We are the processor.') }}</strong>
            {{
              copy(
                'We hold and process the data to run the service for you, on your instructions, and for nothing else.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'Some data is outside this split. Your own account, workspace configuration, and billing records are data for which we are the controller, described in the',
              )
            }}
            <NuxtLinkLocale to="/privacy">{{
              copy('privacy notice')
            }}</NuxtLinkLocale>
            {{
              copy(
                'rather than here. Where you are yourself a processor for someone else, you confirm you have the authority to appoint us as a sub-processor.',
              )
            }}
          </p>

          <h2 id="instructions">
            {{ copy('Processing on your documented instructions') }}
          </h2>
          <p>
            {{
              copy(
                'We process customer personal data only on your documented instructions, including for international transfers, unless a law we are subject to requires otherwise. Where such a law applies, we will tell you before processing unless that law forbids it.',
              )
            }}
          </p>
          <p>{{ copy('Your instructions are, together:') }}</p>
          <ul>
            <li>{{ copy('The terms of service and this addendum;') }}</li>
            <li>
              {{
                copy(
                  'Your configuration of the service — projects, widget rules, consent modes, retention window, integrations, and whether AI analysis is enabled for a project;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  "Use of the product's documented features and API by your authorised members.",
                )
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'If we believe an instruction infringes data-protection law, we will tell you and may pause the affected processing rather than carry it out.',
              )
            }}
          </p>

          <h2 id="duration">{{ copy('Subject matter and duration') }}</h2>
          <p>
            {{
              copy(
                'The subject matter, nature, purpose, data categories, and categories of data subjects are set out in',
              )
            }}
            <a href="#annex-i">{{ copy('Annex I') }}</a
            >{{
              copy(
                '. Processing lasts as long as your subscription, plus the deletion periods described under',
              )
            }}
            <a href="#deletion">{{ copy('return and deletion') }}</a
            >.
          </p>

          <h2 id="confidentiality">{{ copy('Confidentiality') }}</h2>
          <p>
            {{
              copy(
                'Access to customer personal data is limited to people who need it to run or support the service, and everyone with such access is bound by a duty of confidentiality that survives the end of their engagement.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'Support access to sensitive material — attachments, replays, exports — is logged with actor, timestamp, and purpose. That audit trail is a capability we are still completing; see',
              )
            }}
            <a href="#not-settled">{{ copy('what is not yet settled') }}</a
            >.
          </p>

          <h2 id="security">{{ copy('Security of processing') }}</h2>
          <p>
            {{
              copy(
                'We implement appropriate technical and organisational measures under Article 32, described concretely in',
              )
            }}
            <a href="#annex-ii">{{ copy('Annex II') }}</a
            >{{
              copy(
                '. Those measures are the commitment, not the marketing summary: if we change them, they must remain at least equivalent in protection.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'We hold no SOC 2, ISO 27001, or comparable certification, and we do not imply otherwise anywhere in this document.',
              )
            }}
          </p>

          <h2 id="subprocessors">{{ copy('Sub-processors') }}</h2>
          <p>
            {{
              copy(
                'You give general authorisation for us to engage sub-processors. Those engaged today, and what each one does, are listed in',
              )
            }}
            <a href="#annex-iii">{{ copy('Annex III') }}</a
            >.
          </p>
          <p>
            {{
              copy(
                'Each sub-processor is engaged under a written contract imposing data-protection obligations no less protective than these, and we remain fully liable to you for their performance.',
              )
            }}
          </p>
          <p>
            <strong>{{ copy('Changes.') }}</strong>
            {{
              copy(
                "We will give you at least 30 days' notice before a new sub-processor starts processing customer personal data. If you have a reasonable data-protection objection, tell us within that period and we will work to offer a change; if we cannot, you may terminate the affected part of the service and receive a pro-rata refund for the unused period. Silence for 30 days is acceptance.",
              )
            }}
          </p>

          <h2 id="transfers">{{ copy('International transfers') }}</h2>
          <p>
            {{
              copy(
                'Where a sub-processor processes customer personal data outside the country you are in, including outside the European Economic Area, that transfer requires a valid mechanism — an adequacy decision, Standard Contractual Clauses, or another lawful route — together with a transfer impact assessment.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'We do not claim EU data residency for flows that do not support it, and we do not claim transfer mechanisms we have not confirmed. The per-provider position is recorded in',
              )
            }}
            <a href="#annex-iii">{{ copy('Annex III') }}</a
            >{{
              copy(
                ', and where it is still unverified the annex says so rather than filling the gap.',
              )
            }}
          </p>

          <h2 id="assistance">
            {{ copy('Assisting you with data-subject requests') }}
          </h2>
          <p>
            {{
              copy(
                'Reporters usually contact the company whose product they used, which is you. If a reporter contacts us directly about data we hold for you, we will not answer on your behalf; we will route the request to you and tell the reporter we have done so.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'Taking into account the nature of the processing, we assist you with appropriate technical and organisational measures in meeting requests for access, rectification, erasure, restriction, portability, and objection. The service provides self-serve export and deletion for feedback, attachments, and replays, which is normally enough to answer a request without our involvement. Where it is not, ask us.',
              )
            }}
          </p>

          <h2 id="breach">{{ copy('Personal data breach') }}</h2>
          <p>
            {{
              copy(
                'We notify you without undue delay after becoming aware of a personal data breach affecting customer personal data, so that you can meet your own Article 33 deadline. Notification goes to the contact your workspace holds.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'The notification describes, as far as we know it at the time:',
              )
            }}
          </p>
          <ul>
            <li>
              {{
                copy(
                  'The nature of the breach and the categories and approximate volume affected;',
                )
              }}
            </li>
            <li>{{ copy('The likely consequences;') }}</li>
            <li>
              {{
                copy(
                  'Measures taken or proposed, including to mitigate adverse effects;',
                )
              }}
            </li>
            <li>{{ copy('A contact point for further information.') }}</li>
          </ul>
          <p>
            {{
              copy(
                'We keep an internal breach register recording incident facts, assessments, chronology, containment, and post-incident actions, held behind an administrative boundary separate from the tenant runtime. Notifications sent to you are preserved as frozen snapshots, so what you were told and when is reconstructable rather than a matter of memory.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                "Notifying a supervisory authority or affected individuals is the controller's decision and remains yours. We will not make it for you.",
              )
            }}
          </p>

          <h2 id="dpia">{{ copy('Data protection impact assessments') }}</h2>
          <p>
            {{
              copy(
                'We provide reasonable assistance with data protection impact assessments and prior consultation under Articles 35 and 36, limited to information about our processing that you cannot reasonably obtain yourself. The',
              )
            }}
            <NuxtLinkLocale to="/security">{{
              copy('security page')
            }}</NuxtLinkLocale
            >{{
              copy(
                ', this addendum, and its annexes are written to answer most of what an assessment asks.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'Session replay and AI analysis are the two capabilities most likely to require an assessment on your side. Both are off by default and become active only through a decision you make.',
              )
            }}
          </p>

          <h2 id="deletion">{{ copy('Return and deletion') }}</h2>
          <p>
            {{
              copy(
                "During your subscription you can export and delete customer personal data yourself at any time. Replays and attachments are removed automatically once they pass your plan's retention window by a daily cleanup, and that deletion is permanent.",
              )
            }}
          </p>
          <p>
            {{
              copy(
                'On termination, deleting a workspace starts a seven-day grace period, after which the data is removed. For 30 days after termination we will help you export data on request. We delete existing copies unless a law we are subject to requires storage, in which case we tell you what is retained and why.',
              )
            }}
          </p>

          <h2 id="audit">{{ copy('Audits and information') }}</h2>
          <p>
            {{
              copy(
                'We make available the information needed to demonstrate compliance with Article 28, and allow for and contribute to audits, including inspections, conducted by you or an auditor you mandate.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                "In practice: written questionnaires and this documentation come first, and we will answer them properly. An on-site or hands-on audit is available where a supervisory authority requires it or after a breach affecting your data, on reasonable notice, no more than once a year unless the law demands otherwise, and subject to confidentiality and to not compromising other customers' data.",
              )
            }}
          </p>

          <h2 id="ai">{{ copy('AI sub-processing') }}</h2>
          <p>
            {{
              copy(
                'AI analysis is disabled by default and is enabled per project by you. When it is on, free-text messages and the media a reporter chose to submit may be sent to Google Gemini for cleaning, transcription, sentiment scoring, and moderation flags.',
              )
            }}
          </p>
          <ul>
            <li>
              <strong>{{
                copy('Session replays are never sent to any AI provider.')
              }}</strong>
            </li>
            <li>
              {{
                copy(
                  'Only the input categories explicitly enabled for the matching rule are sent, and authorisation is carried by the rule rather than assumed from the plan;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Files placed with the provider target immediate deletion, tracked by a retry ledger;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Our stored evidence of AI processing records the provider, model, purpose, category, and status — never the prompt, the source content, or raw provider output;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Derived output follows the same retention, export, and deletion rules as its source.',
                )
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'AI output is advisory. It can be wrong, and it should not be the sole basis for a decision producing legal or similarly significant effects for a person.',
              )
            }}
          </p>
          <p>
            {{
              copy(
                'The downstream contractual position with the AI provider is among the items under',
              )
            }}
            <a href="#not-settled">{{ copy('what is not yet settled') }}</a
            >{{
              copy(
                ', and you should read that before enabling AI on a project carrying personal data.',
              )
            }}
          </p>

          <h2 id="liability">
            {{ copy('Liability and order of precedence') }}
          </h2>
          <p>
            {{ copy('The liability limits in the') }}
            <NuxtLinkLocale to="/tos">{{
              copy('terms of service')
            }}</NuxtLinkLocale>
            {{
              copy(
                "apply to this addendum, except where data-protection law does not permit them to. Nothing here limits a data subject's rights or a supervisory authority's powers.",
              )
            }}
          </p>
          <p>
            {{
              copy(
                'If this addendum conflicts with the terms of service on the processing of customer personal data, this addendum wins. A separately signed data processing agreement wins over both.',
              )
            }}
          </p>

          <h2 id="annex-i">
            {{ copy('Annex I — Description of the processing') }}
          </h2>
          <h3>{{ copy('Subject matter and nature') }}</h3>
          <p>
            {{
              copy(
                'Collecting product feedback through a widget you install, storing it, making it available to your authorised members for triage, and — where you enable them — analysing it, recording sessions, sending updates to reporters, and forwarding notifications to systems you connect.',
              )
            }}
          </p>
          <h3>{{ copy('Purpose') }}</h3>
          <p>
            {{
              copy(
                'Providing the Fidibeki service to you, and supporting you in using it.',
              )
            }}
          </p>
          <h3>{{ copy('Categories of data subjects') }}</h3>
          <ul>
            <li>
              {{
                copy(
                  'Reporters — the users of your product who submit feedback;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Your workspace members, in their capacity as users of the service;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Any individual a reporter refers to in free text or captures in media.',
                )
              }}
            </li>
          </ul>
          <h3>{{ copy('Categories of personal data') }}</h3>
          <ul>
            <li>{{ copy('Free-text feedback, ratings, and reactions;') }}</li>
            <li>
              {{
                copy(
                  'Attachments a reporter chose to add: screenshots, audio, video, element captures;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Session replay, where you enabled it and the reporter consented — inputs masked, identity-bearing attributes stripped, canvas pixels suppressed;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Reporter email address, only where the reporter opted in to updates about their feedback;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Technical context: page origin, browser and device characteristics, and locally computed interaction signals;',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Workspace member identifiers, roles, and activity records.',
                )
              }}
            </li>
          </ul>
          <p>
            <strong>{{ copy('Special categories.') }}</strong>
            {{
              copy(
                'Free text and media can contain health, political, religious, biometric, sexual, employment, or child-related information, because a reporter can type or capture anything. This is not data the service solicits, and configuring rules so that it is not solicited is your obligation under the terms.',
              )
            }}
          </p>
          <h3>{{ copy('Frequency and duration') }}</h3>
          <p>
            {{
              copy(
                "Continuous for the duration of the subscription. Retention follows your plan's window for replays and attachments — 30, 90, or 365 days — with raw widget analytics events kept for 90 days and daily aggregates for two years.",
              )
            }}
          </p>

          <h2 id="annex-ii">
            {{ copy('Annex II — Technical and organisational measures') }}
          </h2>
          <p>
            {{
              copy(
                'These are implemented today. Where something is still being built, it is marked as such here rather than listed as if it were finished.',
              )
            }}
          </p>
          <ul>
            <li>
              <strong>{{ copy('Tenant isolation.') }}</strong>
              {{
                copy(
                  'Application data is stored under enforced row-level security in PostgreSQL; media and replay objects use tenant-prefixed keys in private, S3-compatible storage;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Authentication.') }}</strong>
              {{
                copy(
                  'Passwordless magic links, brief audience-scoped tokens, and rotating refresh cookies with reuse detection — token reuse outside a narrow window revokes the whole session family. SAML SSO is available, and a workspace can require it and disable local login;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Authorisation.') }}</strong>
              {{
                copy(
                  'Explicit per-route permissions across projects, rules, feedback, issues, sessions, members, API keys, integrations, and account operations. Invitations can be restricted to verified company email domains;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Upload pipeline.') }}</strong>
              {{
                copy(
                  'Attachments upload through brief presigned URLs, are verified for size and MIME type, and land quarantined until signature verification passes. Storage objects stay private; downloads and playback stream through the authenticated API rather than direct storage links;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Encryption.') }}</strong>
              {{
                copy(
                  'Integration secrets and custom headers are encrypted at rest with AES-256-GCM and shown exactly once at creation. Outbound webhooks are signed with HMAC-SHA256. Transport is encrypted in transit;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Abuse resistance.') }}</strong>
              {{
                copy(
                  'Public submission endpoints enforce per-IP and per-project rate ceilings, verify browser origin against your allowed domains, and apply daily AI cost budgets. Limiter identities are brief HMAC hashes, and diagnostics never persist raw network or visitor data;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Capture minimisation.') }}</strong>
              {{
                copy(
                  'The widget never reads input values, private page text, or network payloads, and builds no visitor fingerprint. Durable widget storage exists only after purpose-specific consent;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Telemetry scrubbing.') }}</strong>
              {{
                copy(
                  'Error events are filtered by a deterministic allowlist before leaving the application: request URLs, headers, cookies, bodies, user and IP data, breadcrumbs, and free-text exception messages are removed or replaced with fixed placeholders;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Breach register.') }}</strong>
              {{
                copy(
                  'Incident facts, assessments, chronology, containment and post-incident actions are recorded append-only behind a platform-administration boundary excluded from the tenant runtime role;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Deletion.') }}</strong>
              {{
                copy(
                  'A daily cleanup enforces plan retention windows for replays and attachments; workspace deletion runs on a seven-day grace period;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Access logging — in progress.') }}</strong>
              {{
                copy(
                  'Logging of views and downloads of attachments, replays, and exports with actor, timestamp, and purpose is being completed. Treat it as a roadmap item, not a control you can rely on today.',
                )
              }}
            </li>
          </ul>

          <h2 id="annex-iii">{{ copy('Annex III — Sub-processors') }}</h2>
          <p>
            {{
              copy(
                'Roles and purposes below are accurate. The location, legal entity, and transfer mechanism columns a complete annex would carry are',
              )
            }}
            <a href="#not-settled">{{ copy('not yet verified') }}</a
            >{{
              copy(', and are deliberately left unstated rather than guessed.')
            }}
          </p>
          <ul>
            <li>
              <strong>{{
                copy(
                  'Managed hosting, PostgreSQL, Redis, and S3-compatible object storage.',
                )
              }}</strong>
              {{
                copy(
                  'Running the service and storing application data, media, and replay objects;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Stripe.') }}</strong>
              {{ copy('Billing and subscription processing;') }}
            </li>
            <li>
              <strong>{{ copy('Resend.') }}</strong>
              {{ copy('Transactional email delivery;') }}
            </li>
            <li>
              <strong>{{ copy('Sentry.') }}</strong>
              {{
                copy(
                  'Error telemetry, receiving only allowlist-scrubbed events;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Cloudflare Turnstile.') }}</strong>
              {{
                copy(
                  "Signup bot protection, receiving the challenge token and the submitting request's IP address;",
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Google Gemini.') }}</strong>
              {{
                copy(
                  'AI analysis, only for projects where you enable it, and never for session replays;',
                )
              }}
            </li>
            <li>
              <strong>{{
                copy(
                  'Slack, Microsoft Teams, and webhook endpoints you configure.',
                )
              }}</strong>
              {{
                copy(
                  "Only where you connect them, and only the fields the destination's allowlist permits. These act on your instruction and are governed by your relationship with them.",
                )
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'Umami analytics runs on the marketing site only, with visitor consent, and processes no customer personal data. It is listed in the',
              )
            }}
            <NuxtLinkLocale to="/privacy">{{
              copy('privacy notice')
            }}</NuxtLinkLocale>
            {{ copy('rather than here.') }}
          </p>

          <h2 id="not-settled">{{ copy('What is not yet settled') }}</h2>
          <p>
            {{
              copy(
                'A processor agreement is worth less than nothing if it asserts things nobody has checked. These items are open, and we would rather you knew that before relying on this document:',
              )
            }}
          </p>
          <ul>
            <li>
              <strong>{{
                copy(
                  'Sub-processor locations, legal entities, and contract status.',
                )
              }}</strong>
              {{
                copy(
                  'Not yet independently verified across providers. Until they are, Annex III states roles only;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Transfer mechanisms.') }}</strong>
              {{
                copy(
                  'Which providers rely on adequacy, which on Standard Contractual Clauses, and the transfer impact assessment for each, are not yet documented per provider;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy("The AI provider's downstream terms.") }}</strong>
              {{
                copy(
                  'The executed agreement, data retention and logging settings, and zero-data-retention status with the AI provider are not yet confirmed. If your feedback carries personal data you consider sensitive, leave AI analysis off for that project until this is closed;',
                )
              }}
            </li>
            <li>
              <strong>{{
                copy(
                  'Maximum retention periods for several internal record types.',
                )
              }}</strong>
              {{
                copy(
                  'Plan windows for replays and attachments are enforced in code; some audit, delivery, workflow, and evidence records have a bounded lifecycle that is implemented but no approved maximum period;',
                )
              }}
            </li>
            <li>
              <strong>{{
                copy('Sensitive-category processing instructions.')
              }}</strong>
              {{
                copy(
                  'Project-level controls making AI use an explicit privacy decision rather than a plan default are still being built;',
                )
              }}
            </li>
            <li>
              <strong>{{ copy('Certification.') }}</strong>
              {{
                copy('No SOC 2, ISO 27001, or equivalent, and none claimed.')
              }}
            </li>
          </ul>
          <p>
            {{
              copy(
                'We will update this addendum as these close, and tell you about material changes the same way we would any other change to the terms.',
              )
            }}
          </p>

          <h2 id="contact">{{ copy('Contact') }}</h2>
          <p>
            {{
              copy(
                'For questions about this addendum, a signed copy, a security questionnaire, or a sub-processor objection, write to',
              )
            }}
            <a href="mailto:privacy@fidibeki.com">{{
              copy('privacy@fidibeki.com')
            }}</a
            >{{ copy('. Every contact route is on the') }}
            <NuxtLinkLocale to="/contact">{{
              copy('contact page')
            }}</NuxtLinkLocale
            >.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      :title="copy('Reviewing us for a purchase?')"
      :body="
        copy(
          'Bring the questionnaire. We would rather answer the hard questions before you buy than after.',
        )
      "
    />
  </div>
</template>
