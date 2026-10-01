<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { trialDays } from '~/data/pricing'

const activeId = ref('agreement')
let observer: IntersectionObserver | null = null

const headings = [
  { id: 'agreement', label: 'The agreement' },
  { id: 'who-we-are', label: 'Who you contract with' },
  { id: 'eligibility', label: 'Who may use it' },
  { id: 'accounts', label: 'Accounts' },
  { id: 'trial-and-plans', label: 'Trial and plans' },
  { id: 'billing', label: 'Billing' },
  { id: 'cancellation', label: 'Cancellation' },
  { id: 'limits', label: 'Limits and retention' },
  { id: 'your-data', label: 'Your data' },
  { id: 'widget-obligations', label: 'Running the widget' },
  { id: 'replay-consent', label: 'Replay consent' },
  { id: 'ai', label: 'AI processing' },
  { id: 'testimonials', label: 'Testimonials' },
  { id: 'acceptable-use', label: 'Acceptable use' },
  { id: 'integrations', label: 'Integrations' },
  { id: 'ip', label: 'Intellectual property' },
  { id: 'availability', label: 'Availability' },
  { id: 'suspension', label: 'Suspension' },
  { id: 'warranties', label: 'Warranties' },
  { id: 'liability', label: 'Liability' },
  { id: 'indemnity', label: 'Indemnity' },
  { id: 'changes', label: 'Changes' },
  { id: 'governing-law', label: 'Governing law' },
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
  title: 'Terms of Service',
  description:
    'The agreement between you and Fidibeki: what you get, what you owe, what you promise about the feedback you collect, and what happens when either side walks away.',
  path: '/tos',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Terms of Service', path: '/tos' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      eyebrow="Trust"
      title="Terms of Service"
      lede="The agreement between you and Fidibeki, in the same plain language as the rest of this site: what you get, what you owe, what you promise about the feedback you collect, and what happens when either side walks away."
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[{ name: 'Home', path: '/' }, { name: 'Terms of Service', path: '/tos' }]"
        />
      </template>
    </PageHero>

    <section class="section legal-section">
      <div class="container legal-layout">
        <nav class="legal-nav" aria-label="Terms sections">
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
              This is the current version of the terms. Previous versions will be archived here
              when the terms change materially.
            </p>
          </details>

          <h2 id="agreement">The agreement</h2>
          <p>
            For the purposes of these Terms, "Fidibeki", "we", "us", and "our" refer to the Fidibeki
            service operated by ACIDINEY ALVARO CARVALHO SOARES DIAS, an independent service
            provider established in Portugal. Fidibeki is the name of that service, not a company or
            a separate legal entity.
          </p>
          <p>
            These terms are a binding contract between you and us. They apply when you create a
            workspace, install the widget, browse this site, or use anything else we operate. If you
            are agreeing on behalf of a company, you confirm you have the authority to bind it, and
            "you" means that company.
          </p>
          <p>
            Two other documents form part of this contract: the
            <NuxtLink to="/privacy">privacy notice</NuxtLink>, which describes how personal data is
            handled, and our <NuxtLink to="/dpa">Data Processing Addendum</NuxtLink>, which governs
            feedback collected through your widget. Where a signed order form or DPA conflicts with
            these terms, that signed document wins.
          </p>
          <p>
            If you do not accept these terms, do not use Fidibeki. Continuing to use a workspace
            after a material change takes effect is how you accept the revised version.
          </p>

          <h2 id="who-we-are">Who you are contracting with</h2>
          <p>
            Your counterparty is a person, not a company. The Fidibeki service is operated by
            ACIDINEY ALVARO CARVALHO SOARES DIAS, an independent service provider established in
            Portugal, who is the contracting party under these terms and carries the obligations in
            them personally. There is no separate legal entity behind the Fidibeki name, and none
            should be inferred from it.
          </p>
          <p>
            Our tax identification and registration details appear on every invoice we issue. We
            will also provide them on request to anyone with a reason to ask — write to
            <a href="mailto:support@fidibeki.com">support@fidibeki.com</a> and we will send them.
          </p>
          <address class="trader-address">
            Travessa Quinta do Casal 18<br />
            3810-928 Aveiro<br />
            Portugal
          </address>

          <h2 id="eligibility">Who may use Fidibeki</h2>
          <p>
            You must be at least 16 years old to create a workspace, and old enough under your local
            law to enter a contract. Fidibeki is a business tool; it is not intended for children,
            and the widget must not be used to collect personal data from children.
          </p>
          <p>
            You may not use the service if you are subject to sanctions that would make providing it
            unlawful, or if we have previously terminated your workspace for a breach of these
            terms.
          </p>

          <h2 id="accounts">Accounts and workspaces</h2>
          <p>
            Your workspace is yours to administer. You choose who joins it, what role they hold, and
            what they can see. You are responsible for what your members do, for keeping sign-in
            links and API credentials confidential, and for telling us promptly at
            <a href="mailto:support@fidibeki.com">support@fidibeki.com</a> if you believe an account
            has been compromised.
          </p>
          <p>Sign-in works one of two ways, and each puts the keys somewhere different:</p>
          <ul>
            <li>
              <strong>Passwordless email links.</strong> The default. Anyone who can read a member's
              inbox can reach the workspace, so inbox security is workspace security;
            </li>
            <li>
              <strong>SAML single sign-on.</strong> Where you configure it, your identity provider
              decides who gets in. Granting, revoking, and deprovisioning access becomes your
              responsibility, and a stale account at your provider is a live account here. You may
              also require SSO and switch off email links entirely.
            </li>
          </ul>
          <p>
            If you require SSO, the workspace owner keeps an audited break-glass path back in, so a
            certificate expiry or provider outage cannot lock you out permanently. That path is
            deliberately narrow and every use of it is logged. Do not treat it as a second everyday
            login.
          </p>
          <p>
            We are not liable for loss caused by credentials or identity-provider configuration you
            failed to protect, though nothing here limits our own obligation to run the service
            securely.
          </p>

          <h2 id="trial-and-plans">Trial and plans</h2>
          <p>
            Every new workspace starts with a {{ trialDays }}-day free trial. No credit card is
            required to begin, and we do not charge you automatically when the trial ends. If you do
            not choose a plan, the workspace becomes read-only rather than billed.
          </p>
          <p>
            Current plans, prices, and included limits are listed on the
            <NuxtLink to="/pricing">pricing page</NuxtLink>, which forms part of these terms. Plans
            below Enterprise are self-serve; Enterprise is agreed separately and may carry its own
            order form.
          </p>

          <h2 id="billing">Billing, taxes, and renewal</h2>
          <p>
            Payments are processed by Stripe. We do not see or store your full card details. By
            subscribing you authorise recurring charges to your chosen payment method until you
            cancel.
          </p>
          <ul>
            <li>
              <strong>Billing period.</strong> Subscriptions renew automatically each month or each
              year, depending on the term you pick, until cancelled;
            </li>
            <li>
              <strong>Yearly terms.</strong> Annual billing is discounted against the monthly rate.
              The discount applies to the term you paid for and does not survive a mid-term
              downgrade;
            </li>
            <li>
              <strong>Taxes.</strong> Prices exclude VAT and any other applicable tax. Where we are
              required to collect tax, it is added at checkout and shown on your invoice. If you are
              a business in another EU member state, supply a valid VAT number so the correct
              treatment applies;
            </li>
            <li>
              <strong>Failed payments.</strong> If a charge fails, we retry it and email you. If it
              stays unpaid, we may downgrade or suspend the workspace after notice;
            </li>
            <li>
              <strong>Price changes.</strong> We may change prices with at least 30 days' notice
              before your next renewal. The new price applies from that renewal, never
              retroactively.
            </li>
          </ul>

          <h2 id="cancellation">Cancellation, refunds, and withdrawal</h2>
          <p>
            You can cancel at any time from your workspace settings. Cancellation stops the next
            renewal; it does not refund the period already running, and you keep access until the
            end of that period.
          </p>
          <p>
            <strong>If you are a consumer</strong> in the European Union, you normally have 14 days
            to withdraw from a distance contract. Because Fidibeki is a digital service supplied
            immediately, you are asked to expressly request immediate performance and to acknowledge
            that doing so ends the withdrawal right once the service has been fully supplied. This
            does not affect the {{ trialDays }}-day free trial, which lets you evaluate the product
            before paying anything.
          </p>
          <p>
            Outside that right, fees are non-refundable except where the law requires otherwise or
            where we have failed to provide the service. If you think you were billed in error,
            write to <a href="mailto:support@fidibeki.com">support@fidibeki.com</a> and we will look
            at it properly.
          </p>

          <h2 id="limits">Plan limits and data retention</h2>
          <p>
            Each plan includes a monthly feedback allowance, a project count, a member count, and a
            retention window for session replays and attachments of 30, 90, or 365 days. A daily
            cleanup removes replays and attachments past your plan's window. Deletion is permanent
            and is not reversed by a later upgrade, so export anything you need to keep.
          </p>
          <p>
            If you exceed a limit, we will tell you rather than silently discard data. We may pause
            new submissions on the affected project until you upgrade or the next billing period
            starts. We do not bill surprise overage charges.
          </p>

          <h2 id="your-data">Your data and who controls it</h2>
          <p>
            You own your customer data: the feedback you collect, the attachments reporters submit,
            your workspace configuration, and anything you export. We claim no ownership of it.
          </p>
          <p>
            You grant us a limited licence to host, process, transmit, and display that data solely
            to run the service for you, to provide support, and to meet legal obligations. That
            licence ends when the data is deleted.
          </p>
          <p>
            For feedback collected through your widget, you are the data controller and we act as
            your processor under the DPA. That split matters: the lawful basis for collecting
            feedback from your users is yours to establish, not ours. For your own account and
            billing data, we are the controller, as set out in the
            <NuxtLink to="/privacy">privacy notice</NuxtLink>.
          </p>
          <p>
            We do not sell customer data, and we do not use the contents of your feedback to train
            models for ourselves or anyone else.
          </p>

          <h2 id="widget-obligations">Running the widget responsibly</h2>
          <p>
            The widget sits on your product and speaks to your users, so some obligations are
            necessarily yours. By installing it you undertake to:
          </p>
          <ul>
            <li>
              Give your users the notice your own privacy law requires, and name Fidibeki as a
              processor where that is required;
            </li>
            <li>
              Establish a lawful basis for the feedback you collect, and obtain consent where
              consent is the basis;
            </li>
            <li>
              Configure rules so you do not solicit special-category data — health, political,
              religious, biometric, sexual, or employment information — unless you have the
              additional legal grounds to handle it;
            </li>
            <li>Keep the widget off pages aimed at children;</li>
            <li>
              Handle requests from your own users, and route them to us promptly where we hold the
              data.
            </li>
          </ul>
          <p>
            We build the privacy controls; you decide how to use them. If your configuration puts
            you in breach of data-protection law, that is a risk we cannot absorb on your behalf.
          </p>

          <h2 id="replay-consent">Session replay and consent</h2>
          <p>
            Session replay is disabled by default for every project and rule. Where you enable it,
            capture does not begin until the end user gives explicit, affirmative consent — either
            through the prompt we show on your behalf, or through your own consent manager calling
            our runtime API with a structured grant.
          </p>
          <p>
            You must not attempt to defeat that gate: no forging consent grants, no misrepresenting
            purpose or notice version, and no enabling replay on pages where recording would be
            unlawful or would capture data your users have not agreed to share. Replays are never
            sent to any AI provider. Breaking the consent model is a material breach of these terms
            and grounds for immediate suspension.
          </p>

          <h2 id="ai">AI processing</h2>
          <p>
            On eligible plans, and only for projects where you switch it on, free-text messages and
            the attachments a reporter chose to submit may be sent to Google Gemini for cleaning,
            transcription, sentiment scoring, and moderation flags. AI processing is a choice you
            make per project, not a plan default that happens to you.
          </p>
          <p>
            AI output is a suggestion, not a verdict. Sentiment scores, summaries, transcriptions,
            and moderation flags can be wrong, and you should not rely on them alone for decisions
            that affect a person's rights, employment, or access to a service. Derived AI output
            follows the same retention, export, and deletion rules as the submission it came from.
          </p>

          <h2 id="testimonials">Testimonials and published feedback</h2>
          <p>
            The testimonial feature lets you publish feedback a reporter submitted. Publishing
            someone's words and likeness is a separate decision from collecting them, and the
            permission to do it must come from that person. You are responsible for holding that
            permission and for honouring a later withdrawal by unpublishing.
          </p>
          <p>
            You also grant us permission to identify you as a customer — your name and logo — in
            our own marketing, and you can withdraw that permission at any time by emailing us.
          </p>

          <h2 id="acceptable-use">Acceptable use</h2>
          <p>You agree not to:</p>
          <ul>
            <li>Break the law, infringe someone's rights, or help anyone else do either;</li>
            <li>
              Upload malware, or use the widget to phish, harvest credentials, or deceive your
              users about what is being collected;
            </li>
            <li>
              Probe, scan, or attack the service, or circumvent rate limits, plan limits, workspace
              isolation, or consent gates;
            </li>
            <li>
              Resell, sublicense, or run the service as a competing product, or benchmark it for a
              competitor without our written agreement;
            </li>
            <li>Reverse engineer the service, except where the law expressly permits it;</li>
            <li>
              Send us data you have no right to send, or use Fidibeki to store data it was never
              designed to hold, such as payment card numbers or government identifiers.
            </li>
          </ul>
          <p>
            Responsible security research is welcome. Report findings to
            <a href="mailto:security@fidibeki.com">security@fidibeki.com</a> and give us a fair
            chance to fix them; we will not pursue researchers who act in good faith and do not
            exfiltrate other customers' data.
          </p>

          <h2 id="integrations">Third-party services and integrations</h2>
          <p>
            Connecting Slack, Teams, an issue tracker, or a webhook endpoint sends your data to a
            service we do not control, at your instruction. Those services have their own terms and
            privacy practices, and once data reaches them it is governed by that relationship rather
            than this one.
          </p>
          <p>
            We may change or drop an integration if the provider changes their platform. Where an
            integration is central to your plan, we will give reasonable notice first.
          </p>

          <h2 id="ip">Intellectual property</h2>
          <p>
            Fidibeki — the platform, the widget, the documentation, the brand, and everything we
            built to run them — remains ours. These terms grant you a limited, non-exclusive,
            non-transferable right to use the service during your subscription, and nothing more.
          </p>
          <p>
            If you send us ideas or suggestions about Fidibeki itself, we may use them without
            obligation or payment. That covers product feedback about our software only; it does not
            touch the feedback your own users submit through your widget.
          </p>

          <h2 id="availability">Availability and support</h2>
          <p>
            We work to keep Fidibeki available and fast, but we do not offer a contractual uptime
            guarantee on self-serve plans, and we would rather say so than publish a number we
            cannot stand behind. Enterprise agreements can include a service level commitment.
          </p>
          <p>
            Support runs by email at
            <a href="mailto:support@fidibeki.com">support@fidibeki.com</a>. We aim to respond
            within one business day. Planned maintenance is announced in advance where it is likely
            to be disruptive; emergency maintenance may not be.
          </p>

          <h2 id="suspension">Suspension and termination</h2>
          <p>
            You can close your workspace at any time. Deletion starts a seven-day grace period, and
            after that the data is removed and cannot be restored. Export first.
          </p>
          <p>
            We may suspend or terminate a workspace if you materially breach these terms, if fees
            stay unpaid after notice, if your use puts the service or other customers at risk, or if
            the law requires it. Except where the risk is immediate or legally urgent, we will tell
            you first and give you a reasonable chance to fix the problem.
          </p>
          <p>
            On termination, your right to use the service ends and we delete your data on the
            schedule in the privacy notice. For 30 days after termination we will help you export
            your data if you ask.
          </p>

          <h2 id="warranties">Warranties and disclaimers</h2>
          <p>
            We provide the service with reasonable skill and care, and we warrant that we have the
            right to provide it. Beyond that, and to the extent the law allows, the service is
            provided as it is: we do not warrant that it will be uninterrupted, error-free, or fit
            for a purpose we have not agreed in writing.
          </p>
          <p>
            Nothing in this section removes rights you have as a consumer under Portuguese or EU
            law. Those rights apply whatever this contract says.
          </p>

          <h2 id="liability">Limitation of liability</h2>
          <p>
            Neither party excludes liability for death or personal injury caused by negligence, for
            fraud, for wilful misconduct or gross negligence, or for anything else that cannot
            lawfully be excluded. Portuguese law does not permit those exclusions and we do not
            attempt them.
          </p>
          <p>Subject to that, and to the extent the law allows:</p>
          <ul>
            <li>
              Neither party is liable for indirect or consequential loss, lost profits, lost
              revenue, lost goodwill, or lost or corrupted data arising from a failure to keep your
              own backups or exports;
            </li>
            <li>
              Our total aggregate liability arising out of or relating to the service in any twelve
              month period is limited to the greater of the fees you paid us in the twelve months
              before the event giving rise to the claim, or €100.
            </li>
          </ul>
          <p>
            These limits apply to claims in contract, in tort, and on any other basis. They do not
            apply to your obligation to pay fees, and they do not apply to a consumer's
            non-excludable statutory rights.
          </p>

          <h2 id="indemnity">Indemnity</h2>
          <p>
            If you are using Fidibeki as a business, you will defend and indemnify us against
            third-party claims arising from your customer data, your widget configuration, your
            failure to obtain the notices or consents described above, or your breach of the
            acceptable use section. We will tell you promptly about any such claim, let you control
            the defence, and cooperate reasonably.
          </p>
          <p>This section does not apply to consumers.</p>

          <h2 id="changes">Changes to the service and these terms</h2>
          <p>
            The product changes constantly, which is rather the point. We may add, alter, or remove
            features. Where we remove something material that you rely on, we will give reasonable
            notice, and if the change genuinely damages the service for you, you may cancel and
            receive a pro-rata refund of fees paid for the unused period.
          </p>
          <p>
            We may also update these terms. For material changes we will give at least 30 days'
            notice by email or in the service before they take effect, so you have time to leave if
            you disagree. Minor corrections take effect when published, and the effective date at
            the top of this page always tells you which version is current.
          </p>

          <h2 id="governing-law">Governing law and disputes</h2>
          <p>
            These terms are governed by the law of Portugal. The courts of the <em>comarca</em> of
            Aveiro have exclusive jurisdiction over any dispute arising from them, to the exclusion
            of any other.
          </p>
          <p>
            <strong>If you are a consumer</strong>, that choice does not deprive you of the
            protection of mandatory rules in the country where you live, and you may bring
            proceedings in the courts of your own domicile.
          </p>
          <p>
            Consumers in Portugal may also refer a dispute to an alternative dispute resolution
            body. The residual competent entity is the
            <em>Centro Nacional de Informação e Arbitragem de Conflitos de Consumo</em> (CNIACC).
            Complaints can also be filed through the Portuguese electronic complaints book,
            <em>Livro de Reclamações Eletrónico</em>.
          </p>
          <p>
            Before any of that, please write to us. Most disputes are a misunderstanding that one
            honest email resolves faster than any tribunal.
          </p>

          <h2 id="contact">Contact</h2>
          <p>
            For questions about these terms, billing, or anything else, email
            <a href="mailto:support@fidibeki.com">support@fidibeki.com</a>. For privacy and
            data-rights requests, use
            <a href="mailto:privacy@fidibeki.com">privacy@fidibeki.com</a>. For security reports,
            use <a href="mailto:security@fidibeki.com">security@fidibeki.com</a>.
          </p>
          <p>
            Every contact route, and the operator's details, are on the
            <NuxtLink to="/contact">contact page</NuxtLink>. Guides and what to include in a
            report are on the <NuxtLink to="/support">support page</NuxtLink>.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      title="Still reading? Ask us anything."
      body="Terms are easier to trust when a person will answer questions about them. Ours will."
    />
  </div>
</template>
