<!--
THESIS: Find a person in context, then keep their conversation in view.
OWN-WORLD: The existing console's solid surfaces, Avenir type, teal actions and table density.
STORY: Choose an online user or a conversation, read the history, send a personal message.
FIRST VIEWPORT: Live status above a searchable people list beside the conversation and composer.
FORM: Scoped console extension. The two-column inbox becomes a list-to-thread flow on mobile.
-->
<script setup lang="ts">
import { ArrowLeft, MessageCircle, RefreshCw, Send, Users, X } from '@lucide/vue'
import { reconcileThread, prependHistory, mergeMessages, watchSupport, type LiveState, type OnlineMember, type SupportConversation, type SupportMessage, type SupportThread } from '../../shared/live-support'

const { session } = usePlatformAuth()
const { $platformApi, $refreshPlatformSession } = useNuxtApp()
const canRead = computed(() => session.value.admin?.permissions.includes('platform:support:read'))
const canWrite = computed(() => session.value.admin?.permissions.includes('platform:support:write'))
const view = ref<'online' | 'inbox'>('online')
const users = ref<OnlineMember[]>([])
const conversations = ref<SupportConversation[]>([])
const nextCursor = ref('')
const truncated = ref(false)
const search = ref('')
const state = ref<LiveState>('connecting')
const loading = ref(true)
const error = ref(false)
const onlineError = ref(false)
const threadError = ref(false)
const threadLoading = ref(false)
const olderLoading = ref(false)
const sending = ref(false)
const sendError = ref(false)
const draft = ref('')
const selected = ref<{ tenant_id: string; member_id: string; name: string; email?: string; workspace: string } | null>(null)
const thread = ref<SupportThread>({ conversation: null, messages: [], has_more: false })
const history = ref<HTMLElement | null>(null)
const reply = ref<HTMLTextAreaElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const liveUser = computed(() => users.value.find(user => user.member_id === selected.value?.member_id))
const unreadTotal = computed(() => conversations.value.reduce((sum, conversation) => sum + conversation.unread, 0))
const filteredUsers = computed(() => users.value.filter(user => matches(user.name, user.email, user.workspace)))
const filteredConversations = computed(() => conversations.value.filter(conversation => matches(conversation.member_name, conversation.member_email, conversation.workspace_name)))
const drafts = new Map<string, string>()
let controller: AbortController | null = null
let selectionVersion = 0
let reloadPromise: Promise<void> | null = null
let reloadAgain = false
let onlinePromise: Promise<void> | null = null
let onlineAgain = false
let pendingSend: { client_id: string; body: string; member_id: string; tenant_id: string } | null = null
let nearBottom = true
let refreshTimer: ReturnType<typeof setInterval> | null = null

function matches(...values: Array<string | undefined>) { const q = search.value.trim().toLowerCase(); return !q || values.some(value => value?.toLowerCase().includes(q)) }
function displayTime(value: string) { return new Date(value).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }
function trackScroll() { const el = history.value; nearBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 80 }
function rememberDraft() { if (selected.value) drafts.set(selected.value.member_id, draft.value) }
function back() { if (sending.value) return; rememberDraft(); selected.value = null; selectionVersion++; void nextTick(() => searchInput.value?.focus()) }

async function select(person: NonNullable<typeof selected.value>) {
  if (sending.value) return
  rememberDraft(); selected.value = person; draft.value = drafts.get(person.member_id) || ''; sendError.value = false
  thread.value = { conversation: null, messages: [], has_more: false }; selectionVersion++; nearBottom = true
  await loadThread(true)
  if (selected.value?.member_id === person.member_id) { await nextTick(); reply.value?.focus() }
}
async function loadThread(initial = false) {
  const person = selected.value; if (!person) return
  const version = selectionVersion
  if (initial) threadLoading.value = true
  try {
    const data = await $platformApi<SupportThread>(`/admin/backoffice/support/conversation?tenant_id=${person.tenant_id}&member_id=${person.member_id}`, { signal: controller?.signal })
    if (version !== selectionVersion || controller?.signal.aborted) return
    thread.value = reconcileThread(thread.value, data)
    threadError.value = false
    if (data.conversation && !document.hidden && data.conversation.admin_read_sequence < data.conversation.last_sequence) {
      await $platformApi(`/admin/backoffice/support/conversations/${data.conversation.id}/read`, { method: 'POST', body: { sequence: data.conversation.last_sequence }, signal: controller?.signal })
      const item = conversations.value.find(item => item.id === data.conversation?.id); if (item) item.unread = 0
    }
    if (nearBottom) { await nextTick(); history.value?.scrollTo({ top: history.value.scrollHeight }) }
  } catch { if (version === selectionVersion && !controller?.signal.aborted) threadError.value = true }
  finally { if (version === selectionVersion) threadLoading.value = false }
}
async function reloadOnline(): Promise<void> {
  if (controller?.signal.aborted) return
  if (onlinePromise) { onlineAgain = true; return onlinePromise }
  onlinePromise = (async () => {
    do {
      onlineAgain = false
      try {
        const data = await $platformApi<{ users: OnlineMember[]; truncated: boolean }>('/admin/backoffice/support/online', { signal: controller?.signal })
        if (controller?.signal.aborted) return
        users.value = data.users; truncated.value = data.truncated; onlineError.value = false
      } catch { if (!controller?.signal.aborted) onlineError.value = true }
    } while (onlineAgain && !controller?.signal.aborted)
  })().finally(() => { onlinePromise = null })
  return onlinePromise
}
async function reload(): Promise<void> {
  void reloadOnline()
  if (reloadPromise) { reloadAgain = true; return reloadPromise }
  reloadPromise = (async () => {
    do {
      reloadAgain = false
      try {
        const inbox = await $platformApi<{ conversations: SupportConversation[]; next_cursor: string }>('/admin/backoffice/support/conversations', { signal: controller?.signal })
        if (controller?.signal.aborted) return
        // The latest page is authoritative. Older cached rows may have been
        // read in another tab or removed by retention; load them again on demand.
        conversations.value = inbox.conversations; nextCursor.value = inbox.next_cursor
        error.value = false
        await loadThread()
      } catch { if (!controller?.signal.aborted) error.value = true }
      finally { loading.value = false }
    } while (reloadAgain && !controller?.signal.aborted)
  })().finally(() => { reloadPromise = null })
  return reloadPromise
}
async function loadMoreConversations() {
  if (!nextCursor.value || loading.value) return
  loading.value = true
  try {
    const data = await $platformApi<{ conversations: SupportConversation[]; next_cursor: string }>(`/admin/backoffice/support/conversations?before=${nextCursor.value}`, { signal: controller?.signal })
    const merged = new Map(conversations.value.map(item => [item.id, item])); data.conversations.forEach(item => merged.set(item.id, item))
    conversations.value = [...merged.values()]; nextCursor.value = data.next_cursor
  } catch { error.value = true } finally { loading.value = false }
}
async function loadOlder() {
  if (!thread.value.conversation || olderLoading.value) return
  const version = selectionVersion; olderLoading.value = true
  const height = history.value?.scrollHeight ?? 0
  try {
    const data = await $platformApi<SupportThread>(`/admin/backoffice/support/conversations/${thread.value.conversation.id}?before=${thread.value.messages[0]?.sequence}`, { signal: controller?.signal })
    if (version !== selectionVersion) return
    if (!controller?.signal.aborted) thread.value = prependHistory(thread.value, data)
    await nextTick(); if (history.value) history.value.scrollTop += history.value.scrollHeight - height
  } catch { threadError.value = true } finally { olderLoading.value = false }
}
async function send() {
  const person = selected.value; const body = draft.value.trim()
  if (!person || !canWrite.value || sending.value || !body || Array.from(body).length > 4000 || threadError.value) return
  if (!pendingSend || pendingSend.member_id !== person.member_id || pendingSend.body !== body) pendingSend = { tenant_id: person.tenant_id, member_id: person.member_id, client_id: crypto.randomUUID(), body }
  sending.value = true; sendError.value = false
  try {
    const data = await $platformApi<{ message: SupportMessage }>('/admin/backoffice/support/messages', { method: 'POST', body: pendingSend, signal: controller?.signal })
    thread.value.messages = mergeMessages(thread.value.messages, [data.message]); draft.value = ''; drafts.delete(person.member_id); pendingSend = null; nearBottom = true
    await reload()
  } catch { if (!controller?.signal.aborted) sendError.value = true }
  finally { sending.value = false; await nextTick(); reply.value?.focus() }
}
function askFeedback() { if (selected.value) draft.value = `Hi ${selected.value.name}, how is Fidibeki working for you? Is there anything we could improve?` }
function onVisibility() { if (!document.hidden) void reload() }
onMounted(() => {
  if (!canRead.value) { loading.value = false; return }
  controller = new AbortController(); void reload()
  // Expired presence emits no Redis event; refresh also removes silent disconnects.
  refreshTimer = setInterval(() => { void reloadOnline() }, 20_000)
  document.addEventListener('visibilitychange', onVisibility)
  void watchSupport({ url: '/admin/backoffice/support/events', token: () => session.value.accessToken, refreshAuth: $refreshPlatformSession,
    signal: controller.signal, onChange: () => { void reload() }, onPresence: () => { void reloadOnline() }, onState: value => {
      state.value = value
      if (value === 'forbidden') { controller?.abort(); if (refreshTimer) clearInterval(refreshTimer); users.value = []; conversations.value = []; selected.value = null }
    } })
})
onBeforeUnmount(() => { controller?.abort(); if (refreshTimer) clearInterval(refreshTimer); document.removeEventListener('visibilitychange', onVisibility) })
</script>

<template>
  <section class="support-console">
    <header class="support-page-head"><div><h1>Conversations</h1><p>Reach people while they use Fidibeki.</p></div><span class="support-connection" role="status" :data-live="state === 'connected'"><i />{{ state === 'connected' ? 'Live' : state === 'forbidden' ? 'Access unavailable' : state === 'offline' ? 'Reconnecting…' : 'Connecting…' }}</span></header>
    <p v-if="!canRead || state === 'forbidden'" class="panel support-access" role="alert">You need permission to view platform conversations.</p>
    <template v-else>
      <div v-if="error || onlineError" class="support-error" role="alert">We could not refresh conversations. <button type="button" @click="reload"><RefreshCw :size="14" />Try again</button></div>
      <div class="support-workspace" :data-selected="Boolean(selected)">
        <aside class="support-directory">
          <nav class="support-directory-tabs" aria-label="Conversation lists"><button type="button" :aria-pressed="view === 'online'" @click="view = 'online'"><Users :size="15" />Online <span>{{ users.length }}</span></button><button type="button" :aria-pressed="view === 'inbox'" @click="view = 'inbox'"><MessageCircle :size="15" />Inbox <span v-if="unreadTotal">{{ unreadTotal }}</span></button></nav>
          <div class="support-search"><input ref="searchInput" v-model="search" type="search" aria-label="Search people or workspaces" placeholder="Search people or workspaces"></div>
          <div class="support-people">
            <p v-if="loading && !users.length && !conversations.length" class="support-list-empty">Loading people…</p>
            <template v-else-if="view === 'online'">
              <p v-if="!filteredUsers.length" class="support-list-empty">{{ search ? 'No people match your search.' : 'No one is online right now. Your conversations are still in the inbox.' }}</p>
              <button v-for="user in filteredUsers" :key="user.member_id" type="button" class="support-person" :data-selected="selected?.member_id === user.member_id" :disabled="sending" @click="select({ ...user, workspace: user.workspace })">
                <span class="support-avatar" :data-active="user.state === 'active'">{{ user.name.slice(0, 1).toUpperCase() }}</span><span class="support-person-copy"><strong>{{ user.name }}</strong><span>{{ user.workspace }}</span><small>{{ user.state === 'active' ? 'Active' : 'Idle' }} · {{ user.section }}</small></span>
              </button>
              <p v-if="truncated" class="support-list-empty">Showing the most recent 500 browser sessions.</p>
            </template>
            <template v-else>
              <p v-if="!filteredConversations.length" class="support-list-empty">{{ search ? 'No conversations match your search.' : 'Start by sending someone online a message. Their conversation will stay here.' }}</p>
              <button v-for="conversation in filteredConversations" :key="conversation.id" type="button" class="support-person" :data-selected="selected?.member_id === conversation.member_id" :disabled="sending" @click="select({ tenant_id: conversation.tenant_id, member_id: conversation.member_id, name: conversation.member_name, email: conversation.member_email, workspace: conversation.workspace_name || '' })">
                <span class="support-avatar">{{ conversation.member_name.slice(0, 1).toUpperCase() }}</span><span class="support-person-copy"><strong>{{ conversation.member_name }}</strong><span>{{ conversation.workspace_name }}</span><small class="support-snippet">{{ conversation.preview }}</small></span><b v-if="conversation.unread" class="support-unread">{{ conversation.unread }}</b>
              </button>
              <button v-if="nextCursor" class="support-load-more" type="button" :disabled="loading" @click="loadMoreConversations">Load more conversations</button>
            </template>
          </div>
        </aside>
        <section class="support-thread" aria-label="Selected conversation">
          <div v-if="!selected" class="support-welcome"><MessageCircle :size="36" /><h2>A conversation starts with a person.</h2><p>Choose someone online to ask a question, offer help, or hear how things are going.</p><p class="muted">Only platform staff can start a conversation. Users can reply whenever they return.</p></div>
          <template v-else>
            <header class="support-thread-head"><button class="support-back" type="button" aria-label="Back to people" @click="back"><ArrowLeft :size="18" /></button><div><h2>{{ selected.name }}</h2><p>{{ selected.workspace }}<span v-if="selected.email"> · {{ selected.email }}</span></p><small>{{ liveUser ? `${liveUser.state === 'active' ? 'Active' : 'Idle'} in ${liveUser.section}` : 'Offline · messages will be here when they return' }}</small></div><button class="support-clear" type="button" aria-label="Close conversation" @click="back"><X :size="18" /></button></header>
            <div v-if="threadError" class="support-error" role="alert">This conversation could not be loaded. <button type="button" @click="loadThread(true)">Try again</button></div>
            <div ref="history" class="support-history" role="log" aria-label="Messages" aria-live="polite" @scroll="trackScroll">
              <p v-if="threadLoading" class="support-list-empty">Loading conversation…</p>
              <div v-else-if="!thread.messages.length && !threadError" class="support-first-message"><h3>Say hello to {{ selected.name }}.</h3><p>Your message appears in their app with your name. They can reply or dismiss the preview.</p><button v-if="canWrite" class="button secondary" type="button" @click="askFeedback">Ask for feedback</button></div>
              <button v-if="thread.has_more" class="support-load-more" type="button" :disabled="olderLoading" @click="loadOlder">Load earlier messages</button>
              <article v-for="message in thread.messages" :key="message.id" class="support-bubble" :data-own="message.sender_type === 'admin'"><div><strong>{{ message.sender_name }}</strong><time :datetime="message.created_at">{{ displayTime(message.created_at) }}</time></div><p>{{ message.body }}</p></article>
            </div>
            <form v-if="canWrite" class="support-reply" @submit.prevent="send"><label for="platform-support-message">Message to {{ selected.name }}</label><textarea id="platform-support-message" ref="reply" v-model="draft" rows="3" maxlength="8000" placeholder="Write a personal message…" :disabled="sending || threadLoading || threadError" @keydown.ctrl.enter.prevent="send" @keydown.meta.enter.prevent="send" /><p v-if="sendError" class="support-error" role="alert">Your message was not confirmed. The draft is still here. Try sending again.</p><div><small>{{ Array.from(draft.trim()).length.toLocaleString() }} / 4,000 · Ctrl/⌘ + Enter to send</small><button class="button" type="submit" :disabled="sending || threadLoading || threadError || !draft.trim() || Array.from(draft.trim()).length > 4000"><Send :size="15" />{{ sending ? 'Sending…' : 'Send message' }}</button></div></form>
            <p v-else class="support-access">You have read-only access to conversations.</p>
          </template>
        </section>
      </div>
    </template>
  </section>
</template>

<style scoped>
.support-console { max-width: 1440px; margin: 0 auto; }
.support-page-head { display: flex; justify-content: space-between; align-items: center; gap: 16px; margin-bottom: 22px; }
.support-page-head h1 { margin: 0 0 6px; font-size: 26px; letter-spacing: -.025em; }
.support-page-head p { margin: 0; color: var(--ink-soft); }
.support-connection { display: inline-flex; align-items: center; gap: 7px; color: var(--ink-soft); font-size: 12px; white-space: nowrap; }
.support-connection i { width: 7px; height: 7px; border-radius: 50%; background: var(--amber); }
.support-connection[data-live='true'] i { background: var(--green); }
.support-workspace { display: grid; grid-template-columns: 310px minmax(0, 1fr); height: calc(100dvh - 186px); min-height: 480px; border: 1px solid var(--line); border-radius: var(--radius); background: var(--surface); overflow: hidden; }
.support-directory { display: flex; flex-direction: column; min-height: 0; border-right: 1px solid var(--line); }
.support-directory-tabs { display: flex; gap: 6px; padding: 12px; }
.support-directory-tabs button { display: flex; align-items: center; justify-content: center; flex: 1; gap: 7px; min-height: 38px; border: 0; border-radius: var(--radius); background: transparent; color: var(--ink-soft); font: inherit; font-size: 13px; cursor: pointer; }
.support-directory-tabs button[aria-pressed='true'] { background: var(--surface-soft); color: var(--ink); font-weight: 700; }
.support-directory-tabs span { font-size: 11px; }
.support-search { padding: 0 12px 12px; border-bottom: 1px solid var(--line); }
.support-search input { width: 100%; min-height: 38px; padding: 8px 10px; font: inherit; font-size: 12px; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface); color: var(--ink); }
.support-search input::placeholder, .support-reply textarea::placeholder { color: var(--ink-soft); }
.support-people { overflow-y: auto; min-height: 0; }
.support-person { display: flex; gap: 11px; align-items: flex-start; width: 100%; padding: 17px 14px; border: 0; border-bottom: 1px solid var(--line); background: transparent; color: var(--ink); text-align: left; font: inherit; cursor: pointer; }
.support-person:hover { background: var(--row-hover); }
.support-person[data-selected='true'] { background: var(--teal-soft); }
.support-avatar { display: grid; flex: 0 0 34px; height: 34px; place-items: center; background: var(--surface-soft); border: 1px solid var(--line-strong); border-radius: 50%; font-size: 13px; font-weight: 700; }
.support-avatar[data-active='true'] { border-color: var(--green); color: var(--green); }
.support-person-copy { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 4px; }
.support-person-copy strong { font-size: 13px; overflow-wrap: anywhere; }
.support-person-copy span { font-size: 12px; color: var(--ink-soft); overflow-wrap: anywhere; }
.support-person-copy small { font-size: 11px; color: var(--ink-soft); }
.support-snippet { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.support-unread { font-size: 11px; background: var(--teal-fill); color: var(--on-accent); border-radius: 20px; min-width: 20px; padding: 3px 5px; text-align: center; }
.support-thread { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.support-welcome { max-width: 420px; margin: auto; padding: 28px; text-align: center; color: var(--ink-soft); line-height: 1.65; }
.support-welcome h2 { color: var(--ink); font-size: 22px; line-height: 1.3; margin: 20px 0 12px; }
.support-welcome .muted { font-size: 12px; margin-top: 24px; }
.support-thread-head { display: flex; align-items: center; gap: 12px; padding: 18px 24px; border-bottom: 1px solid var(--line); }
.support-thread-head > div { flex: 1; min-width: 0; }
.support-thread-head h2 { margin: 0 0 5px; font-size: 16px; overflow-wrap: anywhere; }
.support-thread-head p { margin: 0 0 6px; font-size: 12px; color: var(--ink-soft); overflow-wrap: anywhere; }
.support-thread-head small { color: var(--ink-soft); font-size: 11px; }
.support-back, .support-clear { display: grid; place-items: center; width: 36px; height: 36px; border: 0; background: transparent; color: var(--ink-soft); cursor: pointer; }
.support-back { display: none; }
.support-history { display: flex; flex: 1; flex-direction: column; gap: 22px; padding: 24px; min-height: 0; overflow-y: auto; overscroll-behavior: contain; }
.support-bubble { max-width: 85%; align-self: flex-start; }
.support-bubble[data-own='true'] { align-self: flex-end; }
.support-bubble > div { display: flex; flex-wrap: wrap; gap: 8px; align-items: baseline; margin-bottom: 6px; }
.support-bubble strong { font-size: 11px; }
.support-bubble time { font-size: 10px; color: var(--ink-soft); }
.support-bubble p { margin: 0; padding: 12px 15px; background: var(--surface-soft); border-radius: var(--radius); white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.6; }
.support-bubble[data-own='true'] p { background: var(--teal-soft); }
.support-reply { padding: 16px 24px 20px; border-top: 1px solid var(--line); }
.support-reply label { display: block; margin-bottom: 8px; font-size: 12px; font-weight: 650; }
.support-reply textarea { display: block; width: 100%; min-height: 74px; max-height: 180px; resize: vertical; padding: 12px; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface); color: var(--ink); font: inherit; }
.support-reply > div { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-top: 12px; }
.support-reply small { color: var(--ink-soft); font-size: 11px; }
.support-error { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 0; padding: 12px 16px; color: var(--red); background: var(--red-soft); font-size: 13px; }
.support-error button, .support-load-more { display: inline-flex; gap: 5px; align-items: center; padding: 8px; border: 0; background: transparent; color: var(--teal); font: inherit; font-size: 12px; cursor: pointer; }
.support-list-empty, .support-access { margin: 0; padding: 24px; color: var(--ink-soft); line-height: 1.6; font-size: 13px; }
.support-first-message { max-width: 390px; margin: auto; text-align: center; }
.support-first-message h3 { font-size: 18px; }
.support-first-message p { color: var(--ink-soft); line-height: 1.6; }
button:focus-visible, input:focus-visible, textarea:focus-visible { outline: 2px solid var(--teal); outline-offset: -2px; }
@media (max-width: 1000px) { .support-workspace { grid-template-columns: 260px minmax(0, 1fr); } .support-reply, .support-history, .support-thread-head { padding: 16px; } }
@media (max-width: 700px) {
  .support-page-head { align-items: flex-start; margin-bottom: 16px; }
  .support-page-head h1 { font-size: 23px; }
  .support-page-head p { font-size: 12px; }
  .support-workspace { display: flex; height: calc(100dvh - 206px); min-height: 450px; }
  .support-directory, .support-thread { width: 100%; border: 0; }
  .support-workspace:not([data-selected='true']) .support-thread, .support-workspace[data-selected='true'] .support-directory { display: none; }
  .support-back { display: grid; }
  .support-clear { display: none; }
  .support-reply > div { align-items: flex-end; }
  .support-reply small { max-width: 140px; }
}
</style>
