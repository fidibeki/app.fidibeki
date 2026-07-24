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
  state: 'new' | 'active'
  onAccept(): Promise<void>
  onReject(): Promise<void>
  onWithdraw(): Promise<void>
}

export interface ConsentPromptOptions {
  purposes: ConsentPromptPurpose[]
  // Wraps every accept/reject decision so the loader can defer re-synchronization until a
  // multi-purpose batch (accept all / reject all / save) has fully settled.
  runExclusive?<T>(action: () => Promise<T>): Promise<T>
}

interface ResolvedPurpose extends ConsentPromptPurpose {
  notice: ConsentPromptNotice
}

export function mountConsentPrompt(options: ConsentPromptOptions): () => void {
  const purposes: ResolvedPurpose[] = []
  for (const purpose of options.purposes) {
    const locale = purpose.locale.toLowerCase()
    const notice = purpose.consent.notices[locale]
    if (notice) purposes.push({ ...purpose, locale, notice })
  }
  const single = purposes[0]
  if (!single) return () => {}

  const exclusive = options.runExclusive ?? (action => action())
  const language = single.locale.split('-', 1)[0]
  const copy = language === 'pt'
    ? {
        header: 'Escolhas de privacidade',
        launcher: 'Escolhas de privacidade',
        purposeLauncher: {
          widget_analytics: 'Privacidade de análise',
          session_replay: 'Privacidade da reprodução',
        },
        retention: {
          widget_analytics: (days: number) => `Os dados de análise são conservados até ${days} dias.`,
          session_replay: (days: number) => `As interações gravadas são conservadas até ${days} dias.`,
        },
        acceptAll: 'Aceitar tudo',
        rejectAll: 'Recusar tudo',
        save: 'Guardar escolhas',
        policy: 'Política de privacidade',
        close: 'Fechar',
        error: 'Não foi possível guardar a escolha. Tenta novamente.',
      }
    : {
        header: 'Privacy choices',
        launcher: 'Privacy choices',
        purposeLauncher: {
          widget_analytics: 'Analytics privacy',
          session_replay: 'Replay privacy',
        },
        retention: {
          widget_analytics: (days: number) => `Analytics data is retained for up to ${days} days.`,
          session_replay: (days: number) => `Recorded interactions are retained for up to ${days} days.`,
        },
        acceptAll: 'Accept all',
        rejectAll: 'Reject all',
        save: 'Save choices',
        policy: 'Privacy policy',
        close: 'Close',
        error: 'Your choice could not be saved. Please try again.',
      }

  const newPurposes = purposes.filter(purpose => purpose.state === 'new')
  const requesting = newPurposes.length > 0
  const combined = purposes.length > 1
  const launcherLabel = combined ? copy.launcher : copy.purposeLauncher[single.purpose]

  const metaLine = (purpose: ResolvedPurpose) =>
    `${escapeHTML(purpose.consent.controller_name)} · ${escapeHTML(copy.retention[purpose.purpose](purpose.consent.retention_days))}`
  const policyLinks = [...new Set(purposes.map(purpose => purpose.consent.privacy_policy_url))]
    .map(url => `<a href="${escapeAttribute(url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(copy.policy)}</a>`)
    .join(' ')

  const singleBody = `
    <p id="fidibeki-consent-description">${escapeHTML(single.notice.description)}</p>
    <p id="fidibeki-consent-meta" class="meta">${metaLine(single)}</p>
    <p>${policyLinks}</p>
    ${single.state === 'new'
      ? `<div class="actions">
          <button class="decision reject" type="button">${escapeHTML(single.notice.reject_label)}</button>
          <button class="decision accept" type="button">${escapeHTML(single.notice.accept_label)}</button>
        </div>`
      : `<button class="withdraw" type="button" data-purpose="${single.purpose}">${escapeHTML(single.notice.withdraw_label)}</button>`}
  `

  const section = (purpose: ResolvedPurpose) => purpose.state === 'new'
    ? `<section class="purpose" lang="${escapeAttribute(purpose.locale)}">
        <label class="choice">
          <input class="toggle" type="checkbox" data-purpose="${purpose.purpose}">
          <span class="choice-copy">
            <span class="purpose-title">${escapeHTML(purpose.notice.title)}</span>
            <span class="purpose-description">${escapeHTML(purpose.notice.description)}</span>
            <span class="meta">${metaLine(purpose)}</span>
          </span>
        </label>
      </section>`
    : `<section class="purpose" lang="${escapeAttribute(purpose.locale)}">
        <h3 class="purpose-title">${escapeHTML(purpose.notice.title)}</h3>
        <p class="meta">${metaLine(purpose)}</p>
        <button class="withdraw" type="button" data-purpose="${purpose.purpose}">${escapeHTML(purpose.notice.withdraw_label)}</button>
      </section>`

  const combinedBody = `
    ${purposes.map(section).join('')}
    <p>${policyLinks}</p>
    ${requesting
      ? `<div class="actions">
          <button class="decision reject-all" type="button">${escapeHTML(copy.rejectAll)}</button>
          <button class="decision accept-all" type="button">${escapeHTML(copy.acceptAll)}</button>
        </div>
        <button class="save" type="button">${escapeHTML(copy.save)}</button>`
      : ''}
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
      .panel[hidden] { display: none; }
      .header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
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
      h3.purpose-title { margin: 0; font-size: 14px; }
      .purpose-description { display: block; margin-top: 4px; color: #495956; }
      .choice .meta { display: block; margin-top: 4px; }
      .purpose .meta { margin-top: 4px; }
      .actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 16px; }
      .decision:hover { border-color: #087f73; color: #087f73; }
      .save { width: 100%; margin-top: 8px; }
      .save:hover { border-color: #087f73; color: #087f73; }
      .privacy-launcher { width: 100%; padding: 0 14px; border-color: #087f73; color: #087f73; }
      .close { min-width: 32px; min-height: 32px; padding: 0; border: 0; font-size: 22px; line-height: 1; }
      .withdraw { width: 100%; margin-top: 10px; border-color: #9c3f37; color: #87332d; }
      .status { min-height: 18px; margin-top: 8px; color: #9c3f37; font-size: 12px; }
      @media (max-width: 420px) {
        .host { left: 12px; bottom: 12px; width: calc(100vw - 24px); }
        .panel { padding: 16px; }
      }
    </style>
    <div class="host">
      ${requesting ? '' : `<button class="privacy-launcher" type="button">${escapeHTML(launcherLabel)}</button>`}
      <section class="panel" role="dialog" aria-modal="true" lang="${escapeAttribute(single.locale)}" aria-labelledby="fidibeki-consent-title"${combined ? '' : ' aria-describedby="fidibeki-consent-description fidibeki-consent-meta"'} ${requesting ? '' : 'hidden'}>
        <div class="header">
          <h2 id="fidibeki-consent-title">${escapeHTML(combined ? copy.header : single.notice.title)}</h2>
          ${requesting ? '' : `<button class="close" type="button" aria-label="${escapeHTML(copy.close)}">&times;</button>`}
        </div>
        ${combined ? combinedBody : singleBody}
        <div class="status" role="status" aria-live="polite"></div>
      </section>
    </div>
  `
  document.body.append(host)

  const panel = root.querySelector<HTMLElement>('.panel')!
  const status = root.querySelector<HTMLElement>('.status')!
  const controls = () => [...root.querySelectorAll<HTMLButtonElement | HTMLInputElement>('button, input')]
  const run = async (action: () => Promise<void>, wrap = true) => {
    status.textContent = ''
    controls().forEach(control => { control.disabled = true })
    try {
      await (wrap ? exclusive(action) : action())
    } catch {
      status.textContent = copy.error
      controls().forEach(control => { control.disabled = false })
    }
  }
  const decideAll = (decide: (purpose: ResolvedPurpose) => Promise<void>) => async () => {
    for (const purpose of newPurposes) await decide(purpose)
  }

  root.querySelector('.accept')?.addEventListener('click', () => void run(() => single.onAccept()))
  root.querySelector('.reject')?.addEventListener('click', () => void run(() => single.onReject()))
  root.querySelector('.accept-all')?.addEventListener('click', () =>
    void run(decideAll(purpose => purpose.onAccept())))
  root.querySelector('.reject-all')?.addEventListener('click', () =>
    void run(decideAll(purpose => purpose.onReject())))
  root.querySelector('.save')?.addEventListener('click', () =>
    void run(decideAll((purpose) => {
      const toggle = root.querySelector<HTMLInputElement>(`.toggle[data-purpose="${purpose.purpose}"]`)
      return toggle?.checked ? purpose.onAccept() : purpose.onReject()
    })))
  for (const button of root.querySelectorAll<HTMLButtonElement>('.withdraw')) {
    const purpose = purposes.find(candidate => candidate.purpose === button.dataset.purpose)
    // Withdrawal intentionally runs outside the exclusive batch: the loader reacts to the
    // revocation event itself and replaces the prompt while the evidence request completes.
    if (purpose) button.addEventListener('click', () => void run(() => purpose.onWithdraw(), false))
  }

  root.querySelector('.privacy-launcher')?.addEventListener('click', () => {
    panel.hidden = false
    root.querySelector<HTMLButtonElement>('.withdraw')?.focus()
  })
  const closePanel = () => {
    panel.hidden = true
    root.querySelector<HTMLButtonElement>('.privacy-launcher')?.focus()
  }
  root.querySelector('.close')?.addEventListener('click', closePanel)
  root.addEventListener('keydown', (event) => {
    const keyboardEvent = event as KeyboardEvent
    if (keyboardEvent.key !== 'Escape' || requesting || panel.hidden) return
    keyboardEvent.preventDefault()
    closePanel()
  })
  if (requesting) {
    queueMicrotask(() => root.querySelector<HTMLButtonElement>('.reject, .reject-all')?.focus())
  }

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
