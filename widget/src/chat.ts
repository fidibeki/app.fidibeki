import { visitorLocale } from './chat-locale'
import { chatStorageKey, readChatCredential, forgetChatCredential, type ChatCredential } from './chat-session'
import { emptyChat, exportChat, prependChat, reconcileChat, watchSupport, type ChatMessage, type ChatThread, type LiveState } from '../../shared/website-chat'
import { privacySafeLocation } from './privacy-location'
import { contrastRatio, resolveAccentForeground } from './appearance'
import { resolveTheme, subscribeToHostDataTheme, subscribeToSystemTheme } from './theme-resolver'
import { chatStyles } from './chat-styles'
import type { StorageLike } from './storage'
import type { WidgetConfiguration, WidgetRule } from './types'

interface ChatOptions {
  configuration: WidgetConfiguration
  rule: WidgetRule | null
  publicKey: string
  apiBase: URL
  storage: StorageLike
  mountFeedback(target: HTMLElement): Promise<{ mounted: boolean; unmount(): void }>
  fetcher?: typeof fetch
}
type Credential = ChatCredential
const copy = {
  en: { launcher: 'Messages & feedback', messages: 'Messages', feedback: 'Give feedback', close: 'Close conversation', start: 'Send us a message.', startBody: 'Leave a message and the team will reply when they can.', message: 'Your message', messagePlaceholder: 'Write a message…', startSend: 'Send message', waiting: 'Your conversations live here.', waitingBody: 'The team can send you a message while you visit. You can reply after they start a conversation.', reply: 'Your reply', placeholder: 'Write a reply…', send: 'Send reply', sending: 'Sending…', later: 'Replies may come later.', download: 'Download conversation', older: 'Load earlier messages', unavailable: 'Chat is unavailable right now.', retry: 'Try again', error: 'We could not confirm your message. Your draft is still here.', feedbackUnavailable: 'There is no feedback form for this page.', reconnecting: 'Reconnecting…', open: 'Open conversation', dismiss: 'Dismiss preview', downloadError: 'The download could not be completed. Try again.', reset: 'Forget this chat on this browser', you: 'You', loading: 'Loading…' },
  pt: { launcher: 'Mensagens e feedback', messages: 'Mensagens', feedback: 'Dar feedback', close: 'Fechar conversa', start: 'Envie-nos uma mensagem.', startBody: 'Deixe uma mensagem e a equipa responderá quando puder.', message: 'A sua mensagem', messagePlaceholder: 'Escreva uma mensagem…', startSend: 'Enviar mensagem', waiting: 'As suas conversas ficam aqui.', waitingBody: 'A equipa pode enviar-lhe uma mensagem durante a visita. Pode responder depois de a equipa iniciar uma conversa.', reply: 'A sua resposta', placeholder: 'Escreva uma resposta…', send: 'Enviar resposta', sending: 'A enviar…', later: 'A resposta pode demorar.', download: 'Descarregar conversa', older: 'Carregar mensagens anteriores', unavailable: 'O chat está indisponível de momento.', retry: 'Tentar novamente', error: 'Não foi possível confirmar a mensagem. O rascunho foi guardado.', feedbackUnavailable: 'Não existe um formulário de feedback para esta página.', reconnecting: 'A restabelecer ligação…', open: 'Abrir conversa', dismiss: 'Fechar pré-visualização', downloadError: 'Não foi possível descarregar a conversa. Tente novamente.', reset: 'Esquecer este chat neste navegador', you: 'Você', loading: 'A carregar…' },
}

export function mountChat(options: ChatOptions) {
  const fetcher = options.fetcher ?? fetch
  const text = copy[(document.documentElement.lang || options.configuration.default_locale || 'en').toLowerCase().startsWith('pt') ? 'pt' : 'en']
  const storageKey = chatStorageKey(options.configuration.project_id)
  const lifetime = new AbortController()
  const tabID = crypto.randomUUID()
  let credential: Credential | null = null
  let thread = emptyChat()
  let open = false
  let tab: 'messages' | 'feedback' = 'messages'
  let state: LiveState = 'connecting'
  let activeAt = Date.now()
  let sending = false
  let pending: { client_id: string; body: string } | null = null
  let refreshing: Promise<void> | null = null
  let refreshAgain = false
  let sessionPromise: Promise<void> | null = null
  let generation = 0
  let unmountFeedback: (() => void) | null = null
  let feedbackGeneration = 0
  let previewDismissed = 0
  let stream: AbortController | null = null
  let currentRule = options.rule
  let currentConfiguration = options.configuration
  const host = document.createElement('div')
  host.id = 'fidibeki-chat'
  host.setAttribute('data-fidibeki-block', '')
  host.className = 'rr-block'
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML = `<style>${chatStyles}</style>
<button class="launcher" type="button" aria-expanded="false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M20 11.5a8 8 0 0 1-8 8H4v-8a8 8 0 1 1 16 0Z"/><path d="M8 9h8M8 13h5"/></svg><span class="launcher-label"></span><span class="badge" hidden></span></button>
<section class="panel" hidden role="dialog" aria-modal="false"><header class="head"><div class="identity"><img class="brand" alt="" referrerpolicy="no-referrer" hidden><strong class="name"></strong></div><button class="icon close" type="button">×</button></header><nav class="tabs"><button class="tab-messages" type="button"></button><button class="tab-feedback" type="button"></button></nav><p class="notice connection" role="status" hidden></p><div class="messages" role="log" aria-live="polite"></div><div class="tools"><button class="link older" type="button" hidden></button><button class="link download" type="button" hidden></button></div><p class="notice error" role="alert" hidden></p><button class="link retry" type="button" hidden></button><form class="compose" hidden><label for="chat-reply"></label><textarea id="chat-reply" rows="2" maxlength="8000"></textarea><footer><small></small><button class="send" type="submit"></button></footer></form><div class="feedback" hidden></div><button class="link reset" type="button"></button></section>
<aside class="preview" hidden><button class="icon dismiss" type="button">×</button><button class="preview-open" type="button"><strong></strong><p></p><small></small></button></aside>`
  const node = <T extends HTMLElement = HTMLElement>(selector: string) => shadow.querySelector<T>(selector)!
  const launcher = node<HTMLButtonElement>('.launcher')
  const panel = node('.panel')
  const messages = node('.messages')
  const input = node<HTMLTextAreaElement>('textarea')
  const submit = node<HTMLButtonElement>('.send')
  node('.launcher-label').textContent = text.launcher
  launcher.setAttribute('aria-label', text.launcher)
  launcher.title = text.launcher
  node('.name').textContent = options.configuration.chat?.name || 'Messages'
  panel.setAttribute('aria-label', options.configuration.chat?.name || text.messages)
  node('.close').setAttribute('aria-label', text.close)
  node('.dismiss').setAttribute('aria-label', text.dismiss)
  node('.tab-messages').textContent = text.messages
  node('.tab-feedback').textContent = text.feedback
  node('label').textContent = text.reply
  input.placeholder = text.placeholder
  node('.compose small').textContent = text.later
  node('.download').textContent = text.download
  node('.older').textContent = text.older
  node('.reset').textContent = text.reset
  node('.retry').textContent = text.retry
  node('.preview-open small').textContent = text.open
  messages.setAttribute('aria-label', text.messages)
  document.body.append(host)

  function applyAppearance() {
    const appearance = currentRule?.appearance
    const dark = resolveTheme({ fallback: appearance?.theme ?? currentRule?.theme ?? 'auto' }) === 'dark'
    const accent = appearance?.accent_color ?? currentRule?.accent_color ?? '#147d64'
    const background = appearance?.surface_color ?? (dark ? '#171918' : '#ffffff')
    const foreground = appearance?.text_color ?? (dark ? '#f5f7f6' : '#171a19')
    const variables = {
      '--fb-bg': background, '--fb-fg': foreground,
      '--fb-muted': dark ? '#aeb8b4' : '#63706b', '--fb-line': dark ? '#3b413e' : '#d9dfdc',
      '--fb-accent': accent, '--fb-on-accent': appearance?.accent_foreground ?? resolveAccentForeground(accent),
      '--action-ink': contrastRatio(accent, background) >= 4.5 ? accent : foreground,
    }
    for (const [key, value] of Object.entries(variables)) host.style.setProperty(key, value)
    host.style.colorScheme = dark ? 'dark' : 'light'
    host.dataset.side = (appearance?.position ?? currentRule?.position) === 'bottom-left' ? 'left' : 'right'
    const logo = node<HTMLImageElement>('.brand')
    logo.hidden = !appearance?.logo_url
    if (appearance?.logo_url) logo.src = appearance.logo_url
    else logo.removeAttribute('src')
  }
  function fitViewport() {
    const viewport = window.visualViewport
    host.dataset.compact = String((viewport?.height ?? window.innerHeight) <= 480)
    host.style.setProperty('--viewport-height', `${viewport?.height ?? window.innerHeight}px`)
    host.style.setProperty('--keyboard-inset', `${Math.max(0, window.innerHeight - (viewport?.height ?? window.innerHeight) - (viewport?.offsetTop ?? 0))}px`)
  }
  applyAppearance(); fitViewport()
  node<HTMLImageElement>('.brand').onerror = () => { node('.brand').hidden = true }
  const stopSystemTheme = subscribeToSystemTheme(applyAppearance)
  const stopHostTheme = subscribeToHostDataTheme(applyAppearance)
  window.addEventListener('resize', fitViewport)
  window.visualViewport?.addEventListener('resize', fitViewport)
  window.visualViewport?.addEventListener('scroll', fitViewport)

  function error(message = '') { node('.error').textContent = message; node('.error').hidden = !message }
  function storeCredential(value: Credential | null) { credential = value; try { if (value) options.storage.setItem(storageKey, JSON.stringify(value)); else options.storage.removeItem(storageKey) } catch { /* In-memory chat remains available. */ } }
  function readCredential() { return readChatCredential(options.storage, options.configuration.project_id) }
  function endpoint(path: string) { const url = new URL(`widget/chat/${path}`, options.apiBase); url.searchParams.set('key', options.publicKey); return url.href }
  function headers() { return { 'X-Fidibeki-Public-Key': options.publicKey, 'X-Fidibeki-Chat-Token': credential?.token || '', 'Content-Type': 'application/json' } }
  async function request<T>(path: string, method = 'GET', body?: unknown, signal: AbortSignal = lifetime.signal): Promise<T> {
    const response = await fetcher(endpoint(path), { method, headers: headers(), ...(body === undefined ? {} : { body: JSON.stringify(body) }), mode: 'cors', credentials: 'omit', cache: 'no-store', signal })
    if (!response.ok) throw new Error(`Chat request failed (${response.status})`)
    return (response.status === 204 ? undefined : await response.json()) as T
  }
  async function newSession() {
    if (sessionPromise) return sessionPromise
    const version = ++generation
    sessionPromise = (async () => {
      const value = await request<Credential>('session', 'POST', { locale: visitorLocale() })
      if (lifetime.signal.aborted || version !== generation) return
      storeCredential(value); thread = emptyChat(); previewDismissed = 0; pending = null; render()
    })().finally(() => { sessionPromise = null })
    return sessionPromise
  }
  function canSend() { return state !== 'forbidden' && Boolean(thread.conversation || thread.allow_visitor_messages) }
  function render() {
    const nearBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80
    const unread = thread.conversation?.unread ?? 0
    node('.badge').textContent = String(unread); node('.badge').hidden = !unread
    panel.hidden = !open; launcher.setAttribute('aria-expanded', String(open))
    host.dataset.open = String(open)
    host.dataset.view = tab
    node('.tab-messages').setAttribute('aria-pressed', String(tab === 'messages'))
    node('.tab-feedback').setAttribute('aria-pressed', String(tab === 'feedback'))
    messages.hidden = tab !== 'messages'; node('.tools').hidden = tab !== 'messages'; node('.feedback').hidden = tab !== 'feedback'
    node('.reset').hidden = tab !== 'messages'
    node('.compose').hidden = tab !== 'messages' || !canSend()
    node('label').textContent = thread.conversation ? text.reply : text.message
    input.placeholder = thread.conversation ? text.placeholder : text.messagePlaceholder
    node('.download').hidden = !thread.conversation; node('.older').hidden = !thread.has_more
    input.disabled = sending; submit.disabled = sending || !input.value.trim() || Array.from(input.value.trim()).length > 4000
    submit.textContent = sending ? text.sending : thread.conversation ? text.send : text.startSend
    node('.connection').textContent = state === 'forbidden' ? text.unavailable : text.reconnecting
    node('.connection').hidden = tab !== 'messages' || state === 'connected' || state === 'connecting'
    messages.replaceChildren()
    if (!thread.messages.length) {
      const empty = document.createElement('div'); empty.className = 'empty'
      const title = document.createElement('strong'); title.textContent = thread.allow_visitor_messages ? text.start : text.waiting
      const body = document.createElement('p'); body.textContent = thread.allow_visitor_messages ? text.startBody : text.waitingBody
      empty.append(title, body); messages.append(empty)
    }
    for (const message of thread.messages) {
      const article = document.createElement('article'); article.className = `bubble${message.sender_type === 'visitor' ? ' own' : ''}`
      const name = document.createElement('b'); name.textContent = message.sender_type === 'visitor' ? text.you : message.sender_name
      const body = document.createElement('p'); body.textContent = message.body
      const time = document.createElement('time'); time.dateTime = message.created_at; time.textContent = new Date(message.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      article.append(name, body, time); messages.append(article)
    }
    if (nearBottom) messages.scrollTop = messages.scrollHeight
    const incoming = [...thread.messages].reverse().find(message => message.sender_type === 'operator')
    node('.preview').hidden = open || !unread || !incoming || incoming.sequence <= previewDismissed
    if (incoming) { node('.preview-open strong').textContent = incoming.sender_name; node('.preview-open p').textContent = incoming.body }
  }
  async function refresh(): Promise<void> {
    if (refreshing) { refreshAgain = true; return refreshing }
    refreshing = (async () => {
      do {
        refreshAgain = false
        const version = generation
        try {
          const next = await request<ChatThread>('conversation')
          if (version !== generation || lifetime.signal.aborted) return
          thread = reconcileChat(thread, next); node('.retry').hidden = true; render()
          if (open && tab === 'messages' && !document.hidden && next.conversation && next.conversation.visitor_read_sequence < next.conversation.last_sequence) {
            await request('read', 'POST', { sequence: next.conversation.last_sequence })
            if (version === generation && thread.conversation) { thread.conversation.visitor_read_sequence = next.conversation.last_sequence; thread.conversation.unread = 0; render() }
          }
        } catch { if (!lifetime.signal.aborted && version === generation) node('.retry').hidden = false }
      } while (refreshAgain && !lifetime.signal.aborted)
    })().finally(() => { refreshing = null })
    return refreshing
  }
  async function heartbeat() {
    if (!credential || lifetime.signal.aborted || state === 'forbidden') return
    const page = currentRule ? privacySafeLocation(location.href, currentRule)?.target : `${location.origin}/`
    try { await request('presence', 'PUT', { locale: visitorLocale(), tab_id: tabID, page_url: page || `${location.origin}/`, state: !document.hidden && Date.now() - activeAt < 60_000 ? 'active' : 'idle' }) } catch { /* SSE reconnect supplies the recovery path. */ }
  }
  function connect() {
    stream?.abort(); stream = new AbortController()
    void watchSupport({ url: endpoint('events'), token: () => credential?.token || null, headers, fetcher,
      refreshAuth: async () => { await newSession(); await heartbeat() }, signal: stream.signal,
      onChange: () => { void refresh() }, onState: value => { state = value; if (value === 'forbidden') { thread = emptyChat(); input.value = ''; pending = null }; render() },
    })
    void heartbeat(); void refresh()
  }
  async function start() {
    credential = readCredential()
    const initialization = credential ? Promise.resolve() : newSession()
    const version = generation
    try { await initialization; if (!lifetime.signal.aborted && version === generation && credential) connect() }
    catch { if (!lifetime.signal.aborted && version === generation) { state = 'offline'; node('.retry').hidden = false; render() } }
  }
  async function showFeedback() {
    const version = ++feedbackGeneration
    unmountFeedback?.(); unmountFeedback = null
    const target = node('.feedback'); target.replaceChildren(); target.classList.add('feedback-state'); target.textContent = text.loading
    try {
      target.textContent = ''
      const result = await options.mountFeedback(target)
      if (lifetime.signal.aborted || version !== feedbackGeneration) { result.unmount(); return }
      if (!result.mounted) target.textContent = text.feedbackUnavailable
      else { target.classList.remove('feedback-state'); unmountFeedback = result.unmount }
    } catch { if (version === feedbackGeneration) target.textContent = text.feedbackUnavailable }
  }
  function setOpen(value: boolean) {
    open = value; render()
    if (open) { if (tab === 'feedback') void showFeedback(); else { void refresh(); node('.close').focus() } }
    else { ++feedbackGeneration; unmountFeedback?.(); unmountFeedback = null; launcher.focus() }
  }
  launcher.onclick = () => setOpen(!open)
  node('.close').onclick = () => setOpen(false)
  node('.preview-open').onclick = () => { tab = 'messages'; setOpen(true) }
  node('.dismiss').onclick = () => { previewDismissed = thread.conversation?.last_sequence || 0; render() }
  node('.tab-messages').onclick = () => { ++feedbackGeneration; unmountFeedback?.(); unmountFeedback = null; tab = 'messages'; render(); void refresh() }
  node('.tab-feedback').onclick = () => { tab = 'feedback'; render(); void showFeedback() }
  node('.retry').onclick = () => { if (credential) { connect(); void refresh() } else void start() }
  input.oninput = () => { submit.disabled = sending || !input.value.trim() || Array.from(input.value.trim()).length > 4000 }
  input.onkeydown = event => { if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') { event.preventDefault(); node<HTMLFormElement>('form').requestSubmit() } }
  node<HTMLFormElement>('form').onsubmit = async event => {
    event.preventDefault(); const body = input.value.trim()
    if (sending || !canSend() || !body || Array.from(body).length > 4000) return
    if (!pending || pending.body !== body) pending = { body, client_id: crypto.randomUUID() }
    sending = true; error(); render(); const version = generation
    try {
      await request<{ message: ChatMessage }>('messages', 'POST', pending)
      if (version !== generation || lifetime.signal.aborted) return
      input.value = ''; pending = null; await refresh(); messages.scrollTop = messages.scrollHeight
    } catch { if (version === generation && !lifetime.signal.aborted) { error(text.error); await refresh() } }
    finally { sending = false; if (!lifetime.signal.aborted) { render(); input.focus() } }
  }
  node<HTMLButtonElement>('.older').onclick = async () => {
    const version = generation; const before = thread.messages[0]?.sequence
    if (!before) return
    const height = messages.scrollHeight
    try { const older = await request<ChatThread>(`conversation?before=${before}`); if (version !== generation || lifetime.signal.aborted) return; thread = prependChat(thread, older); render(); messages.scrollTop += messages.scrollHeight - height } catch { error(text.unavailable) }
  }
  node<HTMLButtonElement>('.download').onclick = async () => {
    const button = node<HTMLButtonElement>('.download'); button.disabled = true; error()
    try {
      const blob = await exportChat((before, signal) => request<ChatThread>(`conversation${before ? `?before=${before}` : ''}`, 'GET', undefined, signal), lifetime.signal)
      const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = 'website-conversation.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch { if (!lifetime.signal.aborted) error(text.downloadError) } finally { button.disabled = false }
  }
  async function reset() {
    const previous = credential; const version = ++generation
    stream?.abort(); stream = null; credential = null; thread = emptyChat(); input.value = ''; pending = null; previewDismissed = 0; state = 'connecting'; error(); render()
    // Drop the stored credential synchronously, even if the network is offline.
    const forgetting = forgetChatCredential({ storage: options.storage, projectID: options.configuration.project_id, publicKey: options.publicKey, apiBase: options.apiBase, credential: previous, fetcher })
    await Promise.all([forgetting, sessionPromise?.catch(() => {})])
    if (version === generation && !lifetime.signal.aborted) await start()
  }
  node('.reset').onclick = () => { void reset() }
  const activity = () => { activeAt = Date.now() }
  const visibility = () => { void heartbeat(); if (!document.hidden) void refresh() }
  const hide = () => { if (credential) void fetcher(endpoint(`presence/${tabID}`), { method: 'DELETE', headers: headers(), credentials: 'omit', keepalive: true }).catch(() => {}) }
  const storageChanged = (event: StorageEvent) => { if (event.key === storageKey) { generation++; stream?.abort(); thread = emptyChat(); input.value = ''; pending = null; void start() } }
  const escape = (event: KeyboardEvent) => { if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false) } }
  document.addEventListener('pointerdown', activity, { passive: true }); document.addEventListener('keydown', activity, { passive: true })
  document.addEventListener('visibilitychange', visibility); window.addEventListener('pagehide', hide); window.addEventListener('storage', storageChanged)
  shadow.addEventListener('keydown', escape as EventListener)
  const timer = setInterval(() => { void heartbeat() }, 20_000)
  render(); void start()
  return {
    reset,
    update(configuration: WidgetConfiguration, rule: WidgetRule | null) { currentConfiguration = configuration; currentRule = rule; node('.name').textContent = currentConfiguration.chat?.name || text.messages; panel.setAttribute('aria-label', currentConfiguration.chat?.name || text.messages); applyAppearance(); void heartbeat() },
    destroy() { hide(); lifetime.abort(); stream?.abort(); generation++; feedbackGeneration++; unmountFeedback?.(); clearInterval(timer); stopSystemTheme(); stopHostTheme(); window.removeEventListener('resize', fitViewport); window.visualViewport?.removeEventListener('resize', fitViewport); window.visualViewport?.removeEventListener('scroll', fitViewport); document.removeEventListener('pointerdown', activity); document.removeEventListener('keydown', activity); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('pagehide', hide); window.removeEventListener('storage', storageChanged); host.remove() },
  }
}
