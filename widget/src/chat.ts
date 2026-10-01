import { chatStorageKey, readChatCredential, forgetChatCredential, type ChatCredential } from './chat-session'
import { emptyChat, exportChat, prependChat, reconcileChat, watchSupport, type ChatMessage, type ChatThread, type LiveState } from '../../shared/website-chat'
import { privacySafeLocation } from './privacy-location'
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
  en: { launcher: 'Messages & feedback', messages: 'Messages', feedback: 'Give feedback', close: 'Close conversation', waiting: 'Your conversations live here.', waitingBody: 'The team can send you a message while you visit. You can reply after they start a conversation.', reply: 'Your reply', placeholder: 'Write a reply…', send: 'Send reply', sending: 'Sending…', later: 'Replies may come later.', download: 'Download conversation', older: 'Load earlier messages', unavailable: 'Chat is unavailable right now.', retry: 'Try again', error: 'We could not confirm your message. Your draft is still here.', feedbackUnavailable: 'There is no feedback form for this page.', reconnecting: 'Reconnecting…', open: 'Open conversation', dismiss: 'Dismiss preview', downloadError: 'The download could not be completed. Try again.', reset: 'Forget this chat on this browser', you: 'You', loading: 'Loading…' },
  pt: { launcher: 'Mensagens e feedback', messages: 'Mensagens', feedback: 'Dar feedback', close: 'Fechar conversa', waiting: 'As suas conversas ficam aqui.', waitingBody: 'A equipa pode enviar-lhe uma mensagem durante a visita. Pode responder depois de a equipa iniciar uma conversa.', reply: 'A sua resposta', placeholder: 'Escreva uma resposta…', send: 'Enviar resposta', sending: 'A enviar…', later: 'A resposta pode demorar.', download: 'Descarregar conversa', older: 'Carregar mensagens anteriores', unavailable: 'O chat está indisponível de momento.', retry: 'Tentar novamente', error: 'Não foi possível confirmar a mensagem. O rascunho foi guardado.', feedbackUnavailable: 'Não existe um formulário de feedback para esta página.', reconnecting: 'A restabelecer ligação…', open: 'Abrir conversa', dismiss: 'Fechar pré-visualização', downloadError: 'Não foi possível descarregar a conversa. Tente novamente.', reset: 'Esquecer este chat neste navegador', you: 'Você', loading: 'A carregar…' },
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
  shadow.innerHTML = `<style>
:host{all:initial;font:14px/1.5 system-ui,-apple-system,sans-serif;color:#172a2a;--bg:#fff;--ink:#172a2a;--muted:#637171;--line:#dce5e3;--soft:#f1f6f5;--accent:#08796d;--on-accent:#fff;position:fixed;bottom:20px;right:20px;z-index:2147483000;color-scheme:light}
*{box-sizing:border-box}button,textarea{font:inherit}button{cursor:pointer}button:disabled{cursor:default;opacity:.55}button:focus-visible,textarea:focus-visible{outline:3px solid var(--accent);outline-offset:3px}[hidden]{display:none!important}
.launcher{display:flex;align-items:center;gap:8px;border:0;border-radius:24px;background:var(--accent);color:var(--on-accent);padding:12px 18px;box-shadow:0 3px 18px #0002;font-weight:650;min-height:44px}.badge{background:var(--bg);color:var(--accent);border-radius:16px;padding:0 6px;font-size:12px}
.panel{position:absolute;bottom:60px;right:0;width:370px;height:min(600px,calc(100dvh - 110px));display:flex;flex-direction:column;background:var(--bg);color:var(--ink);border:1px solid var(--line);border-radius:14px;box-shadow:0 12px 48px #0003;overflow:hidden}.head{padding:18px;display:flex;align-items:center;justify-content:space-between;gap:12px}.head strong{font-size:17px}.icon{border:0;background:none;color:var(--ink);width:38px;height:38px;font-size:23px}.tabs{display:flex;gap:20px;padding:0 18px;border-bottom:1px solid var(--line)}.tabs button{padding:10px 0;border:0;background:none;color:var(--muted)}.tabs button[aria-pressed=true]{color:var(--accent);box-shadow:0 2px var(--accent)}.messages{flex:1;min-height:0;overflow:auto;padding:18px;display:flex;flex-direction:column;gap:15px;overscroll-behavior:contain}.empty{margin:auto;text-align:center;color:var(--muted)}.empty strong{display:block;color:var(--ink);font-size:16px;margin-bottom:8px}.bubble{max-width:90%;align-self:flex-start}.bubble.own{align-self:flex-end}.bubble b,.bubble time{display:block;font-size:11px;font-weight:500;color:var(--muted)}.bubble p{padding:10px 12px;margin:5px 0;background:var(--soft);border-radius:8px;white-space:pre-wrap;overflow-wrap:anywhere}.bubble.own p{background:#dff3ee;color:#173b34}.compose{padding:12px 16px;border-top:1px solid var(--line)}.compose label{display:block;font-size:12px;margin-bottom:5px}.compose textarea{display:block;width:100%;min-height:64px;max-height:120px;resize:vertical;border:1px solid var(--line);border-radius:6px;padding:8px;background:var(--bg);color:var(--ink)}.compose footer{display:flex;justify-content:space-between;align-items:center;gap:8px;margin-top:9px}.compose small{color:var(--muted);font-size:11px}.send{background:var(--accent);color:var(--on-accent);border:0;border-radius:6px;padding:9px 13px;min-height:40px}.link{border:0;background:none;color:var(--accent);font-size:12px;text-decoration:underline;padding:8px}.notice{font-size:12px;color:var(--muted);padding:8px 16px;margin:0}.feedback{flex:1;min-height:0;overflow:auto;padding:12px}.tools{display:flex;justify-content:space-between;flex-wrap:wrap}.preview{position:absolute;right:0;bottom:60px;width:310px;background:var(--bg);color:var(--ink);box-shadow:0 4px 25px #0003;border:1px solid var(--line);border-radius:12px;padding:16px}.preview .dismiss{position:absolute;right:3px;top:3px}.preview-open{text-align:left;border:0;background:none;color:inherit;padding:0;width:100%}.preview-open strong{display:block;padding-right:24px}.preview-open p{display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere;margin:8px 0}.preview-open small{color:var(--accent)}
@media(prefers-color-scheme:dark){:host{--bg:#182323;--ink:#eef5f3;--muted:#a9bbb7;--line:#3b4e49;--soft:#263933;--accent:#81d9c4;--on-accent:#122a24;color-scheme:dark}}
@media(max-width:430px){:host{bottom:max(12px,env(safe-area-inset-bottom));right:12px}.panel{width:calc(100vw - 24px);height:min(600px,calc(100dvh - 100px))}.preview{max-width:calc(100vw - 24px)}}
</style>
<button class="launcher" type="button" aria-expanded="false"><span class="launcher-label"></span><span class="badge" hidden></span></button>
<section class="panel" hidden role="dialog" aria-modal="false"><header class="head"><strong class="name"></strong><button class="icon close" type="button">×</button></header><nav class="tabs"><button class="tab-messages" type="button"></button><button class="tab-feedback" type="button"></button></nav><p class="notice connection" role="status" hidden></p><div class="messages" role="log" aria-live="polite"></div><div class="tools"><button class="link older" type="button" hidden></button><button class="link download" type="button" hidden></button></div><p class="notice error" role="alert" hidden></p><button class="link retry" type="button" hidden></button><form class="compose" hidden><label for="chat-reply"></label><textarea id="chat-reply" rows="2" maxlength="8000"></textarea><footer><small></small><button class="send" type="submit"></button></footer></form><div class="feedback" hidden></div><button class="link reset" type="button"></button></section>
<aside class="preview" hidden><button class="icon dismiss" type="button">×</button><button class="preview-open" type="button"><strong></strong><p></p><small></small></button></aside>`
  const node = <T extends HTMLElement = HTMLElement>(selector: string) => shadow.querySelector<T>(selector)!
  const launcher = node<HTMLButtonElement>('.launcher')
  const panel = node('.panel')
  const messages = node('.messages')
  const input = node<HTMLTextAreaElement>('textarea')
  const submit = node<HTMLButtonElement>('.send')
  node('.launcher-label').textContent = text.launcher
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
      const value = await request<Credential>('session', 'POST', {})
      if (lifetime.signal.aborted || version !== generation) return
      storeCredential(value); thread = emptyChat(); previewDismissed = 0; pending = null; render()
    })().finally(() => { sessionPromise = null })
    return sessionPromise
  }
  function render() {
    const nearBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80
    const unread = thread.conversation?.unread ?? 0
    node('.badge').textContent = String(unread); node('.badge').hidden = !unread
    panel.hidden = !open; launcher.setAttribute('aria-expanded', String(open))
    node('.tab-messages').setAttribute('aria-pressed', String(tab === 'messages'))
    node('.tab-feedback').setAttribute('aria-pressed', String(tab === 'feedback'))
    messages.hidden = tab !== 'messages'; node('.tools').hidden = tab !== 'messages'; node('.feedback').hidden = tab !== 'feedback'
    node('.compose').hidden = tab !== 'messages' || !thread.conversation || state === 'forbidden'
    node('.download').hidden = !thread.conversation; node('.older').hidden = !thread.has_more
    input.disabled = sending; submit.disabled = sending || !input.value.trim() || Array.from(input.value.trim()).length > 4000
    submit.textContent = sending ? text.sending : text.send
    node('.connection').textContent = state === 'forbidden' ? text.unavailable : text.reconnecting
    node('.connection').hidden = state === 'connected' || state === 'connecting'
    messages.replaceChildren()
    if (!thread.messages.length) {
      const empty = document.createElement('div'); empty.className = 'empty'
      const title = document.createElement('strong'); title.textContent = text.waiting
      const body = document.createElement('p'); body.textContent = text.waitingBody
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
    try { await request('presence', 'PUT', { tab_id: tabID, page_url: page || `${location.origin}/`, state: !document.hidden && Date.now() - activeAt < 60_000 ? 'active' : 'idle' }) } catch { /* SSE reconnect supplies the recovery path. */ }
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
    const target = node('.feedback'); target.replaceChildren(); target.textContent = text.loading
    try {
      target.textContent = ''
      const result = await options.mountFeedback(target)
      if (lifetime.signal.aborted || version !== feedbackGeneration) { result.unmount(); return }
      if (!result.mounted) target.textContent = text.feedbackUnavailable
      else unmountFeedback = result.unmount
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
    if (sending || !thread.conversation || !body || Array.from(body).length > 4000) return
    if (!pending || pending.body !== body) pending = { body, client_id: crypto.randomUUID() }
    sending = true; error(); render(); const version = generation
    try {
      await request<{ message: ChatMessage }>('messages', 'POST', pending)
      if (version !== generation || lifetime.signal.aborted) return
      input.value = ''; pending = null; await refresh(); messages.scrollTop = messages.scrollHeight
    } catch { if (version === generation && !lifetime.signal.aborted) error(text.error) }
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
    update(configuration: WidgetConfiguration, rule: WidgetRule | null) { currentConfiguration = configuration; currentRule = rule; node('.name').textContent = currentConfiguration.chat?.name || text.messages; void heartbeat() },
    destroy() { hide(); lifetime.abort(); stream?.abort(); generation++; feedbackGeneration++; unmountFeedback?.(); clearInterval(timer); document.removeEventListener('pointerdown', activity); document.removeEventListener('keydown', activity); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('pagehide', hide); window.removeEventListener('storage', storageChanged); host.remove() },
  }
}
