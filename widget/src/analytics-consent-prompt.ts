import type { AnalyticsConsentConfig } from './types'

export interface AnalyticsConsentPromptOptions {
  consent: AnalyticsConsentConfig
  locale: string
  state: 'new' | 'active'
  onAccept(): Promise<void>
  onReject(): Promise<void>
  onWithdraw(): Promise<void>
}

// Structurally mirrors replay-consent-prompt.ts's Fidibeki-managed dialog pattern, adapted with
// analytics-specific copy and a distinct dataset attribute/host so the two purposes never share
// DOM state or evidence (issue #58).
export function mountAnalyticsConsentPrompt(options: AnalyticsConsentPromptOptions): () => void {
  const locale = options.locale.toLowerCase()
  const notice = options.consent.notices[locale]
  if (!notice) return () => {}

  const host = document.createElement('div')
  host.dataset.fidibekiAnalyticsConsent = ''
  const root = host.attachShadow({ mode: 'open' })
  const language = locale.split('-', 1)[0]
  const copy = language === 'pt'
    ? {
        launcher: 'Privacidade de análise',
        policy: 'Política de privacidade',
        retention: `Os dados de análise são conservados até ${options.consent.retention_days} dias.`,
        close: 'Fechar',
        error: 'Não foi possível guardar a escolha. Tenta novamente.',
      }
    : {
        launcher: 'Analytics privacy',
        policy: 'Privacy policy',
        retention: `Analytics data is retained for up to ${options.consent.retention_days} days.`,
        close: 'Close',
        error: 'Your choice could not be saved. Please try again.',
      }

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
      button:focus-visible, a:focus-visible { outline: 3px solid rgba(8, 127, 115, .28); outline-offset: 2px; }
      button:disabled { cursor: wait; opacity: .62; }
      .actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 16px; }
      .decision:hover { border-color: #087f73; color: #087f73; }
      .privacy-launcher { width: 100%; padding: 0 14px; border-color: #087f73; color: #087f73; }
      .close { min-width: 32px; min-height: 32px; padding: 0; border: 0; font-size: 22px; line-height: 1; }
      .withdraw { width: 100%; margin-top: 16px; border-color: #9c3f37; color: #87332d; }
      .status { min-height: 18px; margin-top: 8px; color: #9c3f37; font-size: 12px; }
      @media (max-width: 420px) {
        .host { left: 12px; bottom: 12px; width: calc(100vw - 24px); }
        .panel { padding: 16px; }
      }
    </style>
    <div class="host">
      ${options.state === 'active' ? `<button class="privacy-launcher" type="button">${escapeHTML(copy.launcher)}</button>` : ''}
      <section class="panel" role="dialog" aria-modal="true" lang="${escapeAttribute(locale)}" aria-labelledby="analytics-consent-title" aria-describedby="analytics-consent-description analytics-consent-meta" ${options.state === 'active' ? 'hidden' : ''}>
        <div class="header">
          <h2 id="analytics-consent-title">${escapeHTML(notice.title)}</h2>
          ${options.state === 'active' ? `<button class="close" type="button" aria-label="${escapeHTML(copy.close)}">&times;</button>` : ''}
        </div>
        <p id="analytics-consent-description">${escapeHTML(notice.description)}</p>
        <p id="analytics-consent-meta" class="meta">${escapeHTML(options.consent.controller_name)} · ${escapeHTML(copy.retention)}</p>
        <p><a href="${escapeAttribute(options.consent.privacy_policy_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(copy.policy)}</a></p>
        ${options.state === 'new'
          ? `<div class="actions">
              <button class="decision reject" type="button">${escapeHTML(notice.reject_label)}</button>
              <button class="decision accept" type="button">${escapeHTML(notice.accept_label)}</button>
            </div>`
          : `<button class="withdraw" type="button">${escapeHTML(notice.withdraw_label)}</button>`}
        <div class="status" role="status" aria-live="polite"></div>
      </section>
    </div>
  `
  document.body.append(host)

  const panel = root.querySelector<HTMLElement>('.panel')!
  const status = root.querySelector<HTMLElement>('.status')!
  const buttons = () => [...root.querySelectorAll<HTMLButtonElement>('button')]
  const run = async (action: () => Promise<void>) => {
    status.textContent = ''
    buttons().forEach(button => { button.disabled = true })
    try {
      await action()
    } catch {
      status.textContent = copy.error
      buttons().forEach(button => { button.disabled = false })
    }
  }

  root.querySelector('.accept')?.addEventListener('click', () => void run(options.onAccept))
  root.querySelector('.reject')?.addEventListener('click', () => void run(options.onReject))
  root.querySelector('.withdraw')?.addEventListener('click', () => void run(options.onWithdraw))
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
    if (keyboardEvent.key !== 'Escape' || options.state !== 'active' || panel.hidden) return
    keyboardEvent.preventDefault()
    closePanel()
  })
  if (options.state === 'new') {
    queueMicrotask(() => root.querySelector<HTMLButtonElement>('.reject')?.focus())
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
