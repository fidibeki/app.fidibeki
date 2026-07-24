import { resolveAccentForeground } from './appearance'
import { resolveWidgetLocalizationForPage, type ResolvedWidgetLocalization } from './localization'
import { readPageLanguage, subscribeToPageLanguage } from './page-language'
import { createPrivacyEntry } from './privacy-entries'
import type { PrivacyChoice, WidgetConfiguration, WidgetRule } from './types'

export interface PrivacyControlsOptions {
  rule: WidgetRule
  configuration: WidgetConfiguration
  choices(): PrivacyChoice[]
  subscribe(listener: () => void): () => void
  // Called when the control removes itself because there is nothing left to withdraw, so the
  // loader can drop its bookkeeping and mount a fresh control if consent is granted again.
  onRemoved?(): void
}

// The standing withdrawal path for every moment the feedback panel is not on the page: before its
// trigger fires, after a dismissal or a completed submission, and on page views where recurrence
// suppresses it entirely. The loader mounts exactly one of the two (see synchronizePrivacyControl),
// so this control and the feedback launcher never share the same corner.
//
// It borrows the rule's own launcher shape — the configured variant for launcher presentations, an
// edge tab everywhere else — so it reads as part of the same widget rather than a second badge
// dropped over the customer's layout.
export function mountPrivacyControls(options: PrivacyControlsOptions): () => void {
  const appearance = options.rule.appearance
  const accent = appearance?.accent_color ?? options.rule.accent_color ?? '#147d64'
  const accentForeground = appearance?.accent_foreground ?? resolveAccentForeground(accent)
  const side = (appearance?.position ?? options.rule.position) === 'bottom-right' ? 'right' : 'left'
  const variant = options.rule.presentation === 'launcher'
    ? options.rule.launcher_variant ?? 'edge_tab'
    : 'edge_tab'
  const browserLocales = navigator.languages.length > 0 ? navigator.languages : [navigator.language]
  let localization = resolveLocalization(readPageLanguage())

  function resolveLocalization(pageLanguage: string | null): ResolvedWidgetLocalization {
    return resolveWidgetLocalizationForPage(
      options.rule,
      options.configuration.default_locale,
      pageLanguage,
      browserLocales,
    )
  }

  const host = document.createElement('div')
  host.dataset.fidibekiPrivacyControls = ''
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML = `<style>${styles(
    accent, accentForeground, side, appearance?.theme ?? options.rule.theme,
    appearance?.surface_color, appearance?.text_color,
  )}</style>
    <button class="launcher ${variant === 'floating_button' ? 'floating-button' : 'edge-tab'}" type="button" data-privacy-launcher data-launcher-variant="${variant}" aria-expanded="false">${escapeHTML(localization.system.privacyChoices)}</button>
    <section class="panel" data-privacy-panel role="dialog" aria-label="${escapeHTML(localization.system.privacyChoices)}" hidden>
      <header><h2 data-copy="privacy-choices-title">${escapeHTML(localization.system.privacyChoices)}</h2>
        <button class="icon" type="button" data-privacy-close aria-label="${escapeHTML(localization.system.closePrivacyChoices)}" title="${escapeHTML(localization.system.closePrivacyChoices)}">&#215;</button></header>
      <div class="list" data-privacy-list></div>
      <p class="privacy-status" data-privacy-status role="status" aria-live="polite"></p>
    </section>`
  document.body.append(host)

  const launcher = required<HTMLButtonElement>(shadow, '[data-privacy-launcher]')
  const panel = required<HTMLElement>(shadow, '[data-privacy-panel]')
  const closeButton = required<HTMLButtonElement>(shadow, '[data-privacy-close]')
  const list = required<HTMLElement>(shadow, '[data-privacy-list]')
  const status = required<HTMLElement>(shadow, '[data-privacy-status]')
  const cleanups: Array<() => void> = []
  let withdrawing = 0

  const render = (keepStatus = false) => {
    const choices = options.choices()
    if (!keepStatus) status.textContent = ''
    list.replaceChildren(...choices.map(choice => createPrivacyEntry(choice, localization.system, {
      onStatus: (text) => { status.textContent = text },
      onBusyChanged: (busy) => { withdrawing += busy ? 1 : -1 },
      onSettled: () => render(true),
    })))
    // Nothing left to withdraw and nothing left to say: the control removes itself rather than
    // lingering as an empty affordance over the host page.
    if (choices.length === 0 && !status.textContent && withdrawing === 0) destroy(true)
  }

  const close = (returnFocus: boolean) => {
    if (panel.hidden) return
    panel.hidden = true
    launcher.hidden = false
    launcher.setAttribute('aria-expanded', 'false')
    status.textContent = ''
    // The outcome that was keeping an emptied control alive has now been read.
    if (options.choices().length === 0 && withdrawing === 0) {
      destroy(true)
      return
    }
    if (returnFocus && launcher.isConnected) launcher.focus()
  }

  launcher.addEventListener('click', () => {
    render()
    if (!launcher.isConnected) return
    launcher.hidden = true
    panel.hidden = false
    panel.querySelector<HTMLButtonElement>('button')?.focus()
  })
  closeButton.addEventListener('click', () => close(true))
  shadow.addEventListener('keydown', (event) => {
    const keyboardEvent = event as KeyboardEvent
    if (keyboardEvent.key !== 'Escape' || panel.hidden) return
    keyboardEvent.preventDefault()
    close(true)
  })

  cleanups.push(subscribeToPageLanguage((pageLanguage) => {
    localization = resolveLocalization(pageLanguage)
    launcher.textContent = localization.system.privacyChoices
    panel.setAttribute('aria-label', localization.system.privacyChoices)
    required<HTMLElement>(shadow, '[data-copy="privacy-choices-title"]').textContent =
      localization.system.privacyChoices
    closeButton.setAttribute('aria-label', localization.system.closePrivacyChoices)
    closeButton.title = localization.system.closePrivacyChoices
    render(true)
  }))
  cleanups.push(options.subscribe(() => {
    // Re-render only what is open: an unopened launcher reads its list on click, and the emptied
    // case is handled by render() removing the control.
    if (panel.hidden) {
      if (options.choices().length === 0 && withdrawing === 0) destroy(true)
      return
    }
    render(true)
  }))

  let destroyed = false
  function destroy(announce: boolean) {
    if (destroyed) return
    destroyed = true
    for (const cleanup of cleanups) cleanup()
    host.remove()
    if (announce) options.onRemoved?.()
  }
  return () => destroy(false)
}

function required<T extends Element>(root: ShadowRoot, selector: string): T {
  const element = root.querySelector<T>(selector)
  if (!element) throw new Error(`Fidibeki privacy control element missing: ${selector}`)
  return element
}

function styles(
  accent: string,
  accentForeground: string,
  side: 'left' | 'right',
  theme: WidgetRule['theme'],
  surface?: string,
  text?: string,
): string {
  const dark = theme === 'dark'
  const automatic = theme === 'auto' && !surface && !text
  const exposedRadius = side === 'left' ? '0 10px 10px 0' : '10px 0 0 10px'
  return `:host{--fb-accent:${accent};--fb-on-accent:${accentForeground};--fb-bg:${surface ?? (dark ? '#171918' : '#fff')};--fb-fg:${text ?? (dark ? '#f5f7f6' : '#171a19')};--fb-muted:${dark ? '#aeb8b4' : '#63706b'};--fb-line:${dark ? '#3b413e' : '#d9dfdc'};all:initial;display:block;font-family:"Avenir Next","Segoe UI",sans-serif;color:var(--fb-fg)}
  [hidden]{display:none!important}
  .launcher{position:fixed;z-index:2147483000;bottom:20px;${side}:20px;min-height:44px;padding:0 16px;border:1px solid var(--fb-accent);border-radius:6px;background:var(--fb-accent);color:var(--fb-on-accent);font:700 13px/1 sans-serif;letter-spacing:0;box-shadow:0 10px 28px rgba(0,0,0,.18);cursor:pointer}.launcher:focus,.launcher:focus-visible{outline:3px solid var(--fb-on-accent);outline-offset:-5px}
  .launcher.edge-tab{top:50%;bottom:auto;${side}:0;transform:translateY(-50%);min-width:44px;width:44px;min-height:96px;max-height:min(240px,70vh);padding:16px 10px;border-radius:${exposedRadius};writing-mode:vertical-rl;text-orientation:mixed;display:flex;align-items:center;justify-content:center;text-align:center;white-space:normal;overflow:hidden}
  .panel{position:fixed;z-index:2147483000;bottom:20px;${side}:20px;width:min(340px,calc(100vw - 32px));max-height:calc(100dvh - 40px);display:flex;flex-direction:column;box-sizing:border-box;background:var(--fb-bg);color:var(--fb-fg);border:1px solid var(--fb-line);border-top:4px solid var(--fb-accent);border-radius:6px;box-shadow:0 18px 50px rgba(0,0,0,.2);overflow:hidden}
  header{display:flex;flex:none;align-items:flex-start;justify-content:space-between;gap:16px;padding:18px 20px 4px}h2{margin:2px 0 0;font:700 16px/1.3 "Avenir Next","Segoe UI",sans-serif;letter-spacing:0}
  .icon{width:32px;height:32px;display:grid;place-items:center;border:0;background:transparent;color:var(--fb-muted);font-size:24px;line-height:1;cursor:pointer}.icon:hover{color:var(--fb-fg);background:color-mix(in srgb,var(--fb-fg) 7%,transparent)}
  .list{min-height:0;overflow-y:auto;padding:0 20px}
  .privacy-choice{padding:12px 0;border-bottom:1px solid var(--fb-line)}.privacy-choice:last-child{border-bottom:0}.privacy-choice h4{margin:0 0 6px;font:650 13px/1.3 sans-serif}.privacy-choice p{margin:0 0 9px;font:400 12px/1.45 sans-serif;color:var(--fb-muted)}.privacy-choice a{color:var(--fb-accent)}.privacy-choice .privacy-meta{font:400 11px/1.4 sans-serif}
  .secondary{width:100%;min-height:38px;padding:0 12px;border:1px solid var(--fb-line);border-radius:5px;background:transparent;color:var(--fb-fg);font:700 13px/1 sans-serif;letter-spacing:0;cursor:pointer}.secondary:hover{border-color:var(--fb-accent)}.secondary:disabled{opacity:.6;cursor:wait}
  .privacy-status{flex:none;margin:0;padding:8px 20px 16px;font:500 11px/1.4 sans-serif;color:var(--fb-muted)}
  ${automatic ? '@media(prefers-color-scheme:dark){:host{--fb-bg:#171918;--fb-fg:#f5f7f6;--fb-muted:#aeb8b4;--fb-line:#3b413e}}' : ''}
  @media(max-width:480px){.panel{bottom:12px;left:12px;right:12px;width:auto}.launcher{bottom:12px;left:auto;right:auto;${side}:12px}
  .launcher.edge-tab{top:auto;bottom:96px;left:auto;right:auto;${side}:0;transform:none;width:auto;max-width:128px;min-width:44px;min-height:44px;max-height:none;padding:10px 14px;border-radius:${exposedRadius};writing-mode:horizontal-tb;white-space:normal;line-height:1.25;overflow:visible}}
  @media(prefers-reduced-motion:no-preference){.panel{animation:fb-enter .22s ease-out both}@keyframes fb-enter{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:none}}}`
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>'"]/g, (character) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;',
  })[character] ?? character)
}
