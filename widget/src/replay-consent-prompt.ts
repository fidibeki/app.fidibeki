import type { ReplayConsentConfig } from './types'

export interface ReplayConsentPromptOptions {
  consent: ReplayConsentConfig
  locale: string
  onAccept(): Promise<void>
  onReject(): Promise<void>
}

// The dialog asks once and then goes away for good: a granted purpose leaves nothing pinned over
// the host page. Withdrawal lives in the feedback widget's privacy column instead — see
// mountWidget's `privacy` option and loader.ts's privacyChoices.
export function mountReplayConsentPrompt(options: ReplayConsentPromptOptions): () => void {
  const locale = options.locale.toLowerCase()
  const notice = options.consent.notices[locale]
  if (!notice) return () => {}

  const host = document.createElement('div')
  host.dataset.fidibekiReplayConsent = ''
  const root = host.attachShadow({ mode: 'open' })
  const language = locale.split('-', 1)[0]
  const copy = language === 'pt'
    ? {
        policy: 'Política de privacidade',
        retention: `As interações gravadas são conservadas até ${options.consent.retention_days} dias.`,
        error: 'Não foi possível guardar a escolha. Tenta novamente.',
      }
    : {
        policy: 'Privacy policy',
        retention: `Recorded interactions are retained for up to ${options.consent.retention_days} days.`,
        error: 'Your choice could not be saved. Please try again.',
      }

  root.innerHTML = `
    <style>
      :host { all: initial; color-scheme: light; }
      * { box-sizing: border-box; }
      .host {
        position: fixed;
        z-index: 2147483646;
        right: 16px;
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
      button:focus-visible, a:focus-visible { outline: 3px solid rgba(8, 127, 115, .28); outline-offset: 2px; }
      button:disabled { cursor: wait; opacity: .62; }
      .actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 16px; }
      .decision:hover { border-color: #087f73; color: #087f73; }
      .status { min-height: 18px; margin-top: 8px; color: #9c3f37; font-size: 12px; }
      @media (max-width: 420px) {
        .host { right: 12px; bottom: 12px; width: calc(100vw - 24px); }
        .panel { padding: 16px; }
      }
    </style>
    <div class="host">
      <section class="panel" role="dialog" aria-modal="true" lang="${escapeAttribute(locale)}" aria-labelledby="replay-consent-title" aria-describedby="replay-consent-description replay-consent-meta">
        <h2 id="replay-consent-title">${escapeHTML(notice.title)}</h2>
        <p id="replay-consent-description">${escapeHTML(notice.description)}</p>
        <p id="replay-consent-meta" class="meta">${escapeHTML(options.consent.controller_name)} · ${escapeHTML(copy.retention)}</p>
        <p><a href="${escapeAttribute(options.consent.privacy_policy_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(copy.policy)}</a></p>
        <div class="actions">
          <button class="decision reject" type="button">${escapeHTML(notice.reject_label)}</button>
          <button class="decision accept" type="button">${escapeHTML(notice.accept_label)}</button>
        </div>
        <div class="status" role="status" aria-live="polite"></div>
      </section>
    </div>
  `
  document.body.append(host)

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
  queueMicrotask(() => root.querySelector<HTMLButtonElement>('.reject')?.focus())

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
