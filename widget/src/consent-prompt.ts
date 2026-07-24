export type ConsentPromptPurposeID = 'widget_analytics' | 'session_replay'

export interface ConsentPromptNotice {
  title: string
  description: string
  accept_label: string
  reject_label: string
  withdraw_label: string
}

// Structural subset shared by AnalyticsConsentConfig and ReplayConsentConfig. The prompt only
// shares presentation: decisions still flow through the per-purpose callbacks wired in by the
// loader, so each purpose keeps its own controller, storage, and server evidence (issue #58).
export interface ConsentPromptPurposeConfig {
  controller_name: string
  privacy_policy_url: string
  retention_days: number
  notices: Record<string, ConsentPromptNotice>
}

export interface ConsentPromptPurpose {
  purpose: ConsentPromptPurposeID
  consent: ConsentPromptPurposeConfig
  locale: string
  onAccept(): Promise<void>
  onReject(): Promise<void>
}

export interface ConsentPromptOptions {
  purposes: ConsentPromptPurpose[]
  // Wraps every decision so the loader can defer re-synchronization until a multi-purpose batch
  // (accept all / reject all / save) has fully settled.
  runExclusive?<T>(action: () => Promise<T>): Promise<T>
}

interface ResolvedPurpose extends ConsentPromptPurpose {
  notice: ConsentPromptNotice
}

// One dialog for every undecided Fidibeki-managed purpose on the page. It asks once and then
// leaves the host page for good: withdrawal lives in the feedback widget's privacy column and the
// standing privacy control — see mountWidget's `privacy` option and loader.ts's privacyChoices.
export function mountConsentPrompt(options: ConsentPromptOptions): () => void {
  const purposes: ResolvedPurpose[] = []
  for (const purpose of options.purposes) {
    const locale = purpose.locale.toLowerCase()
    const notice = purpose.consent.notices[locale]
    if (notice) purposes.push({ ...purpose, locale, notice })
  }
  const single = purposes.length === 1 ? purposes[0] : null
  if (purposes.length === 0) return () => {}

  const exclusive = options.runExclusive ?? (action => action())
  const language = purposes[0]!.locale.split('-', 1)[0]
  const copy = language === 'pt'
    ? {
        header: 'Escolhas de privacidade',
        retention: {
          widget_analytics: (days: number) => `Os dados de análise são conservados até ${days} dias.`,
          session_replay: (days: number) => `As interações gravadas são conservadas até ${days} dias.`,
        },
        acceptAll: 'Aceitar tudo',
        rejectAll: 'Recusar tudo',
        save: 'Guardar escolhas',
        policy: 'Política de privacidade',
        error: 'Não foi possível guardar a escolha. Tenta novamente.',
      }
    : {
        header: 'Privacy choices',
        retention: {
          widget_analytics: (days: number) => `Analytics data is retained for up to ${days} days.`,
          session_replay: (days: number) => `Recorded interactions are retained for up to ${days} days.`,
        },
        acceptAll: 'Accept all',
        rejectAll: 'Reject all',
        save: 'Save choices',
        policy: 'Privacy policy',
        error: 'Your choice could not be saved. Please try again.',
      }

  const metaLine = (purpose: ResolvedPurpose) =>
    `${escapeHTML(purpose.consent.controller_name)} · ${escapeHTML(copy.retention[purpose.purpose](purpose.consent.retention_days))}`
  const policyLinks = [...new Set(purposes.map(purpose => purpose.consent.privacy_policy_url))]
    .map(url => `<a href="${escapeAttribute(url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(copy.policy)}</a>`)
    .join(' ')

  const singleBody = single
    ? `
    <p id="fidibeki-consent-description">${escapeHTML(single.notice.description)}</p>
    <p id="fidibeki-consent-meta" class="meta">${metaLine(single)}</p>
    <p>${policyLinks}</p>
    <div class="actions">
      <button class="decision reject" type="button">${escapeHTML(single.notice.reject_label)}</button>
      <button class="decision accept" type="button">${escapeHTML(single.notice.accept_label)}</button>
    </div>`
    : ''

  const combinedBody = `
    ${purposes.map(purpose => `
      <section class="purpose" lang="${escapeAttribute(purpose.locale)}">
        <label class="choice">
          <input class="toggle" type="checkbox" data-purpose="${purpose.purpose}">
          <span class="choice-copy">
            <span class="purpose-title">${escapeHTML(purpose.notice.title)}</span>
            <span class="purpose-description">${escapeHTML(purpose.notice.description)}</span>
            <span class="meta">${metaLine(purpose)}</span>
          </span>
        </label>
      </section>`).join('')}
    <p>${policyLinks}</p>
    <div class="actions">
      <button class="decision reject-all" type="button">${escapeHTML(copy.rejectAll)}</button>
      <button class="decision accept-all" type="button">${escapeHTML(copy.acceptAll)}</button>
    </div>
    <button class="save" type="button">${escapeHTML(copy.save)}</button>
  `

  const host = document.createElement('div')
  host.dataset.fidibekiConsent = ''
  const root = host.attachShadow({ mode: 'open' })
  root.innerHTML = `
    <style>
      :host { all: initial; color-scheme: light; }
      * { box-sizing: border-box; }
      .host {
        position: fixed;
        z-index: 2147483646;
        left: 16px;
        bottom: 16px;
        width: min(360px, calc(100vw - 32px));
        color: #17201e;
        font: 14px/1.45 Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      }
      .panel {
        padding: 18px;
        background: #fff;
        border: 1px solid #bdc9c6;
        border-radius: 6px;
        box-shadow: 0 14px 36px rgba(20, 35, 31, .18);
      }
      h2 { margin: 0; color: #17201e; font-size: 17px; line-height: 1.25; letter-spacing: 0; }
      p { margin: 10px 0 0; color: #495956; }
      .meta { color: #667572; font-size: 12px; }
      a { color: #087f73; font-weight: 650; }
      button {
        min-height: 40px;
        border: 1px solid #8fa09c;
        border-radius: 5px;
        background: #fff;
        color: #17201e;
        font: inherit;
        font-weight: 700;
        cursor: pointer;
      }
      button:focus-visible, a:focus-visible, input:focus-visible { outline: 3px solid rgba(8, 127, 115, .28); outline-offset: 2px; }
      button:disabled { cursor: wait; opacity: .62; }
      .purpose { margin-top: 14px; padding-top: 14px; border-top: 1px solid #e2e8e6; }
      .purpose:first-of-type { margin-top: 10px; padding-top: 0; border-top: 0; }
      .choice { display: flex; gap: 10px; cursor: pointer; }
      .choice .toggle { flex: none; margin: 3px 0 0; width: 16px; height: 16px; accent-color: #087f73; }
      .purpose-title { display: block; color: #17201e; font-weight: 700; }
      .purpose-description { display: block; margin-top: 4px; color: #495956; }
      .choice .meta { display: block; margin-top: 4px; }
      .actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 16px; }
      .decision:hover { border-color: #087f73; color: #087f73; }
      .save { width: 100%; margin-top: 8px; }
      .save:hover { border-color: #087f73; color: #087f73; }
      .status { min-height: 18px; margin-top: 8px; color: #9c3f37; font-size: 12px; }
      @media (max-width: 420px) {
        .host { left: 12px; bottom: 12px; width: calc(100vw - 24px); }
        .panel { padding: 16px; }
      }
    </style>
    <div class="host">
      <section class="panel" role="dialog" aria-modal="true" lang="${escapeAttribute(purposes[0]!.locale)}" aria-labelledby="fidibeki-consent-title"${single ? ' aria-describedby="fidibeki-consent-description fidibeki-consent-meta"' : ''}>
        <h2 id="fidibeki-consent-title">${escapeHTML(single ? single.notice.title : copy.header)}</h2>
        ${single ? singleBody : combinedBody}
        <div class="status" role="status" aria-live="polite"></div>
      </section>
    </div>
  `
  document.body.append(host)

  const status = root.querySelector<HTMLElement>('.status')!
  const controls = () => [...root.querySelectorAll<HTMLButtonElement | HTMLInputElement>('button, input')]
  const run = async (action: () => Promise<void>) => {
    status.textContent = ''
    controls().forEach(control => { control.disabled = true })
    try {
      await exclusive(action)
    } catch {
      status.textContent = copy.error
      controls().forEach(control => { control.disabled = false })
    }
  }
  const decideAll = (decide: (purpose: ResolvedPurpose) => Promise<void>) => async () => {
    for (const purpose of purposes) await decide(purpose)
  }

  root.querySelector('.accept')?.addEventListener('click', () => void run(() => single!.onAccept()))
  root.querySelector('.reject')?.addEventListener('click', () => void run(() => single!.onReject()))
  root.querySelector('.accept-all')?.addEventListener('click', () =>
    void run(decideAll(purpose => purpose.onAccept())))
  root.querySelector('.reject-all')?.addEventListener('click', () =>
    void run(decideAll(purpose => purpose.onReject())))
  root.querySelector('.save')?.addEventListener('click', () =>
    void run(decideAll((purpose) => {
      const toggle = root.querySelector<HTMLInputElement>(`.toggle[data-purpose="${purpose.purpose}"]`)
      return toggle?.checked ? purpose.onAccept() : purpose.onReject()
    })))
  queueMicrotask(() => root.querySelector<HTMLButtonElement>('.reject, .reject-all')?.focus())

  return () => host.remove()
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, character => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[character]!)
}

function escapeAttribute(value: string): string {
  return escapeHTML(value)
}
