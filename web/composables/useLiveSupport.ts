import { exportSupportTranscript, reconcileThread, prependHistory, mergeMessages, supportSection, watchSupport, type LiveState, type SupportMessage, type SupportThread } from '../../shared/live-support'

export function useLiveSupport() {
  const { session, authenticated, workspaceWritable } = useAuth()
  const { $apiFetch, $refreshSession } = useNuxtApp()
  const route = useRoute()
  const thread = ref<SupportThread>({ conversation: null, messages: [], has_more: false })
  const state = ref<LiveState>('connecting')
  const loading = ref(true)
  const loadError = ref(false)
  const sending = ref(false)
  const sendError = ref(false)
  const reading = ref(false)
  const olderLoading = ref(false)
  const exporting = ref(false)
  const exportError = ref(false)
  const open = ref(false)
  const tab = ref<'messages' | 'feedback'>('messages')
  const draft = ref('')
  const dismissedSequence = ref(0)
  const unread = computed(() => thread.value.conversation?.unread ?? 0)
  const latestIncoming = computed(() => [...thread.value.messages].reverse().find(message => message.sender_type === 'admin'))
  const preview = computed(() => !open.value && unread.value > 0 && latestIncoming.value && latestIncoming.value.sequence > dismissedSequence.value ? latestIncoming.value : null)
  let controller: AbortController | null = null
  let heartbeat: ReturnType<typeof setInterval> | null = null
  let tabID = ''
  let lastActivity = Date.now()
  let loadingPromise: Promise<void> | null = null
  let loadAgain = false
  let pendingSend: { client_id: string; body: string } | null = null
  let presencePending = false
  let savedToken = ''

  async function markRead() {
    const conversation = thread.value.conversation
    if (!conversation || !open.value || tab.value !== 'messages' || document.hidden || reading.value || state.value === 'forbidden') return
    if (conversation.last_sequence <= conversation.member_read_sequence) return
    const sequence = conversation.last_sequence
    reading.value = true
    try {
      await $apiFetch('/v1/support/read', { method: 'POST', body: { sequence }, signal: controller?.signal })
      if (thread.value.conversation) {
        thread.value.conversation.member_read_sequence = Math.max(thread.value.conversation.member_read_sequence, sequence)
        thread.value.conversation.unread = thread.value.messages.filter(message => message.sender_type === 'admin' && message.sequence > sequence).length
      }
    } catch { /* Retrying on the next snapshot preserves the unread badge. */ }
    finally { reading.value = false }
  }

  async function reload(): Promise<void> {
    if (loadingPromise) { loadAgain = true; return loadingPromise }
    loadingPromise = (async () => {
      do {
        loadAgain = false
        try {
          const data = await $apiFetch<SupportThread>('/v1/support/conversation', { signal: controller?.signal })
          if (controller?.signal.aborted) return
          thread.value = reconcileThread(thread.value, data)
          loadError.value = false
          await markRead()
        } catch { if (!controller?.signal.aborted) loadError.value = true }
        finally { loading.value = false }
      } while (loadAgain && !controller?.signal.aborted)
    })().finally(() => { loadingPromise = null })
    return loadingPromise
  }

  async function loadOlder() {
    if (olderLoading.value || !thread.value.has_more) return
    olderLoading.value = true
    try {
      const data = await $apiFetch<SupportThread>(`/v1/support/conversation?before=${thread.value.messages[0]?.sequence}`, { signal: controller?.signal })
      if (!controller?.signal.aborted) thread.value = prependHistory(thread.value, data)
    } catch { loadError.value = true }
    finally { olderLoading.value = false }
  }

  async function send() {
    const body = draft.value.trim()
    if (!body || Array.from(body).length > 4000 || sending.value || !thread.value.conversation || !workspaceWritable.value) return
    if (!pendingSend || pendingSend.body !== body) pendingSend = { client_id: crypto.randomUUID(), body }
    sending.value = true; sendError.value = false
    try {
      const result = await $apiFetch<{ message: SupportMessage }>('/v1/support/messages', { method: 'POST', body: pendingSend, signal: controller?.signal })
      thread.value.messages = mergeMessages(thread.value.messages, [result.message])
      if (draft.value.trim() === body) draft.value = ''
      pendingSend = null
      await reload()
    } catch { if (!controller?.signal.aborted) sendError.value = true }
    finally { sending.value = false }
  }

  async function downloadTranscript() {
    if (exporting.value || !thread.value.conversation) return
    exporting.value = true; exportError.value = false
    try {
      const blob = await exportSupportTranscript((before, signal) => $apiFetch<SupportThread>(
        `/v1/support/conversation${before ? `?before=${before}` : ''}`, { signal }), controller?.signal)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a'); link.href = url; link.download = 'fidibeki-conversation.json'
      link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch { if (!controller?.signal.aborted) exportError.value = true }
    finally { exporting.value = false }
  }

  async function announce() {
    if (!authenticated.value || !workspaceWritable.value || !tabID || presencePending || controller?.signal.aborted) return
    presencePending = true
    try {
      await $apiFetch('/v1/support/presence', { method: 'PUT', signal: controller?.signal, body: {
        tab_id: tabID, section: supportSection(route.path), state: document.hidden || Date.now() - lastActivity > 60_000 ? 'idle' : 'active',
      } })
    } catch { /* The next heartbeat retries; the server expires stale presence. */ }
    finally { presencePending = false }
  }
  function activity() { lastActivity = Date.now() }
  function visibility() { if (!document.hidden) { activity(); void reload() }; void announce() }
  function leave() {
    if (!tabID || !savedToken) return
    void fetch(`/v1/support/presence/${tabID}`, { method: 'DELETE', keepalive: true,
      headers: { Authorization: `Bearer ${savedToken}` }, credentials: 'same-origin' }).catch(() => {})
  }
  function stop() {
    controller?.abort()
    if (heartbeat) clearInterval(heartbeat)
    leave()
    window.removeEventListener('pointerdown', activity)
    window.removeEventListener('keydown', activity)
    window.removeEventListener('pagehide', leave)
    document.removeEventListener('visibilitychange', visibility)
  }
  watch(() => session.value.accessToken, token => { if (token) savedToken = token })
  watch([open, tab], () => { void markRead() })
  watch(() => route.path, () => { void announce() })
  onMounted(() => {
    if (!authenticated.value || !workspaceWritable.value) return
    controller = new AbortController(); tabID = crypto.randomUUID(); savedToken = session.value.accessToken ?? ''
    window.addEventListener('pointerdown', activity, { passive: true })
    window.addEventListener('keydown', activity)
    window.addEventListener('pagehide', leave)
    document.addEventListener('visibilitychange', visibility)
    heartbeat = setInterval(() => { void announce() }, 20_000)
    void announce(); void reload()
    void watchSupport({ url: '/v1/support/events', token: () => session.value.accessToken, refreshAuth: $refreshSession,
      signal: controller.signal, onChange: () => { void reload() }, onState: value => {
        state.value = value
        if (value === 'forbidden') { thread.value = { conversation: null, messages: [], has_more: false }; stop() }
      } })
  })
  onBeforeUnmount(stop)
  return { thread, state, loading, loadError, sending, sendError, olderLoading, open, tab, draft, unread, preview,
    reload, send, loadOlder, exporting, exportError, downloadTranscript, dismissPreview: () => { dismissedSequence.value = latestIncoming.value?.sequence ?? 0 } }
}
