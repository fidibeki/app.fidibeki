<script setup lang="ts">
import { ArrowLeft, MessageCircle, RefreshCw, Send, Users, X } from '@lucide/vue'
import { emptyChat, exportChat, prependChat, reconcileChat, watchSupport, type ChatConversation, type ChatThread, type ChatVisitor, type LiveState } from '../../shared/website-chat'
import type { Project } from '~/types/api'

const { t } = useI18n()
const { session, can, canWrite: permissionWrite } = useAuth()
const { $apiFetch, $refreshSession } = useNuxtApp()
const route = useRoute()
const projects = ref<Project[]>([])
const projectID = ref('')
const enabled = ref(false)
const settingsBusy = ref(false)
const loadingProjects = ref(true)
const state = ref<LiveState>('connecting')
const view = ref<'online' | 'inbox'>('online')
const visitors = ref<ChatVisitor[]>([])
const conversations = ref<ChatConversation[]>([])
const nextCursor = ref('')
const moreLoading = ref(false)
let inboxPages = 1
let wantedInboxPages = 1
const truncated = ref(false)
const search = ref('')
const selected = ref<{ id: string; name: string } | null>(null)
const thread = ref<ChatThread>(emptyChat())
const draft = ref('')
const sending = ref(false)
const threadLoading = ref(false)
const olderLoading = ref(false)
const downloading = ref(false)
const deleteOpen = ref(false)
const deleting = ref(false)
const deleteError = ref('')
const error = ref('')
const threadError = ref(false)
const sendError = ref(false)
const history = ref<HTMLElement | null>(null)
const reply = ref<HTMLTextAreaElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const canRead = computed(() => can('chat:read') && can('project:read'))
const canSend = computed(() => enabled.value && permissionWrite('chat:write') && (!thread.value.conversation || Date.parse(thread.value.conversation.expires_at) > Date.now()))
const liveVisitor = computed(() => visitors.value.find(visitor => visitor.id === selected.value?.id))
const filteredVisitors = computed(() => visitors.value.filter(visitor => `${visitor.name} ${visitor.page_url}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const filteredConversations = computed(() => conversations.value.filter(conversation => conversation.name.toLowerCase().includes(search.value.trim().toLowerCase())))
const unread = computed(() => conversations.value.reduce((total, conversation) => total + conversation.unread, 0))
const connectionLabel = computed(() => state.value === 'connected' ? t('chat.live') : state.value === 'forbidden' ? t('chat.unavailable') : state.value === 'offline' ? t('chat.reconnecting') : t('chat.connecting'))
const settingLabel = computed(() => enabled.value ? t('chat.disable') : t('chat.enable'))
const settingHint = computed(() => enabled.value ? t('chat.enabledHint') : t('chat.disabledHint'))
const onlineEmpty = computed(() => search.value ? t('chat.noMatch') : enabled.value ? t('chat.noOnline') : t('chat.disabled'))
const inboxEmpty = computed(() => search.value ? t('chat.noMatch') : t('chat.noInbox'))
const replyUnavailable = computed(() => !enabled.value ? t('chat.disabled') : !permissionWrite('chat:write') ? t('chat.readOnly') : t('chat.expired'))
const sendLabel = computed(() => sending.value ? t('chat.sending') : t('chat.send'))
function visitorState(value: string) { return value === 'active' ? t('chat.active') : t('chat.idle') }
const drafts = new Map<string, string>()
let controller: AbortController | null = null
let projectVersion = 0
let selectionVersion = 0
let reloadPromise: Promise<void> | null = null
let reloadAgain = false
let onlineBusy = false
let pending: { id: string; body: string; client_id: string } | null = null
let nearBottom = true
let timer: ReturnType<typeof setInterval> | null = null
const base = () => `/v1/projects/${projectID.value}/chat`
function time(value: string) { return new Date(value).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }
function trackScroll() { const el = history.value; nearBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 80 }
function remember() { if (selected.value) drafts.set(`${projectID.value}:${selected.value.id}`, draft.value) }
function back() { if (sending.value) return; remember(); selected.value = null; selectionVersion++; olderLoading.value = false; threadLoading.value = false; void nextTick(() => searchInput.value?.focus()) }
async function select(person: { id: string; name: string }) {
 if (sending.value) return
 remember(); selected.value = person; draft.value = drafts.get(`${projectID.value}:${person.id}`) || ''; sendError.value = false
 thread.value = emptyChat(); selectionVersion++; olderLoading.value = false; threadLoading.value = false; threadError.value = false; nearBottom = true
 const version = selectionVersion
 await loadThread(true)
 await nextTick(); if (version === selectionVersion) reply.value?.focus()
}
async function loadThread(initial = false) {
 const person = selected.value; if (!person || !controller) return
 const version = selectionVersion; const control = controller; const path = `${base()}/visitors/${person.id}`
 if (initial) threadLoading.value = true
 try {
  const data = await $apiFetch<ChatThread>(path, { signal: control.signal })
  if (version !== selectionVersion || control.signal.aborted) return
  thread.value = reconcileChat(thread.value, data); threadError.value = false
  if (data.conversation && !document.hidden && data.conversation.operator_read_sequence < data.conversation.last_sequence) {
   await $apiFetch(`${path}/read`, { method: 'POST', body: { sequence: data.conversation.last_sequence }, signal: control.signal })
   if (version !== selectionVersion || control.signal.aborted) return
   const item = conversations.value.find(item => item.id === person.id); if (item) item.unread = 0
  }
  if (nearBottom) { await nextTick(); if (version === selectionVersion && !control.signal.aborted) history.value?.scrollTo({ top: history.value.scrollHeight }) }
 } catch { if (version === selectionVersion && !control.signal.aborted) threadError.value = true }
 finally { if (version === selectionVersion) threadLoading.value = false }
}
async function reloadOnline() {
 if (!controller || controller.signal.aborted || onlineBusy) return
 const control = controller; const version = projectVersion; onlineBusy = true
 try {
  const data = await $apiFetch<{ visitors: ChatVisitor[]; truncated: boolean }>(`${base()}/online`, { signal: control.signal })
  if (version === projectVersion && !control.signal.aborted) { visitors.value = data.visitors; truncated.value = data.truncated }
 } catch { if (!control.signal.aborted) error.value = t('chat.error') }
 finally { if (version === projectVersion) onlineBusy = false }
}
async function loadInbox(path: string, signal: AbortSignal, pageCount: number) {
 const rows = new Map<string, ChatConversation>(); const cursors = new Set<string>(); let cursor = ''; let pages = 0
 do {
  const data = await $apiFetch<{ conversations: ChatConversation[]; next_cursor: string }>(`${path}/conversations${cursor ? `?before=${encodeURIComponent(cursor)}` : ''}`, { signal })
  for (const item of data.conversations) rows.set(item.id, item)
  cursor = data.next_cursor; pages++
  if (cursor && cursors.has(cursor)) throw new Error('Inbox pagination did not advance')
  if (cursor) cursors.add(cursor)
 } while (cursor && pages < pageCount)
 return { conversations: [...rows.values()], next_cursor: cursor, pages }
}
function reload(): Promise<void> {
 const control = controller
 if (!control || control.signal.aborted) return Promise.resolve()
 if (reloadPromise) { reloadAgain = true; return reloadPromise }
 const version = projectVersion; const path = base()
 const operation = (async () => {
  try {
   do {
    reloadAgain = false; const pageTarget = wantedInboxPages
    const [settings, inbox] = await Promise.all([
     $apiFetch<{ enabled: boolean }>(`${path}/settings`, { signal: control.signal }),
     loadInbox(path, control.signal, pageTarget),
    ])
    if (version !== projectVersion || control.signal.aborted) return
    enabled.value = settings.enabled; conversations.value = inbox.conversations; nextCursor.value = inbox.next_cursor; inboxPages = inbox.pages
    if (wantedInboxPages === pageTarget) wantedInboxPages = inbox.pages
    error.value = ''; await loadThread(); void reloadOnline()
   } while (reloadAgain && !control.signal.aborted)
  } catch { if (version === projectVersion && !control.signal.aborted) { wantedInboxPages = inboxPages; error.value = t('chat.error') } }
 })()
 const pendingReload = operation.finally(() => { if (reloadPromise === pendingReload) reloadPromise = null })
 reloadPromise = pendingReload
 return pendingReload
}
async function changeSetting() {
 if (settingsBusy.value || !permissionWrite('project:update')) return
 const control = controller; settingsBusy.value = true
 try { const result = await $apiFetch<{ enabled: boolean }>(`${base()}/settings`, { method: 'PUT', body: { enabled: !enabled.value }, signal: control?.signal }); if (!control?.signal.aborted) { enabled.value = result.enabled; await reload() } }
 catch { if (!control?.signal.aborted) error.value = t('chat.settingsError') }
 finally { settingsBusy.value = false }
}
async function older() {
 if (!selected.value || !thread.value.has_more || olderLoading.value) return
 const version = selectionVersion; const control = controller; olderLoading.value = true; const height = history.value?.scrollHeight || 0
 try {
  const data = await $apiFetch<ChatThread>(`${base()}/visitors/${selected.value.id}?before=${thread.value.messages[0]?.sequence}`, { signal: control?.signal })
  if (version !== selectionVersion || control?.signal.aborted) return
  thread.value = prependChat(thread.value, data); await nextTick(); if (version === selectionVersion && !control?.signal.aborted && history.value) history.value.scrollTop += history.value.scrollHeight - height
 } catch { if (version === selectionVersion && !control?.signal.aborted) threadError.value = true }
 finally { if (version === selectionVersion) olderLoading.value = false }
}
async function more() {
 if (!nextCursor.value || moreLoading.value) return
 const version = projectVersion; moreLoading.value = true; wantedInboxPages = inboxPages + 1
 try { await reload() } finally { if (version === projectVersion) moreLoading.value = false }
}

async function send() {
 const person = selected.value; const body = draft.value.trim(); const control = controller
 if (!person || sending.value || !canSend.value || !body || Array.from(body).length > 4000 || threadError.value) return
 if (!pending || pending.id !== person.id || pending.body !== body) pending = { id: person.id, body, client_id: crypto.randomUUID() }
 sending.value = true; sendError.value = false
 try {
  await $apiFetch(`${base()}/visitors/${person.id}/messages`, { method: 'POST', body: { body: pending.body, client_id: pending.client_id }, signal: control?.signal })
  if (control?.signal.aborted) return
  draft.value = ''; drafts.delete(`${projectID.value}:${person.id}`); pending = null; nearBottom = true; await reload()
 } catch { if (!control?.signal.aborted) sendError.value = true }
 finally { sending.value = false; await nextTick(); reply.value?.focus() }
}
async function download() {
 if (!selected.value || downloading.value) return
 const path = `${base()}/visitors/${selected.value.id}`; downloading.value = true
 try { const blob = await exportChat((before, signal) => $apiFetch<ChatThread>(`${path}${before ? `?before=${before}` : ''}`, { signal }), controller?.signal); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = 'website-conversation.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) }
 catch { if (!controller?.signal.aborted) error.value = t('chat.downloadError') }
 finally { downloading.value = false }
}
async function removeSelected() {
 if (!selected.value || deleting.value || !permissionWrite('chat:delete')) return
 const path = `${base()}/visitors/${selected.value.id}`; const control = controller
 deleting.value = true; deleteError.value = ''
 try { await $apiFetch(path, { method: 'DELETE', signal: control?.signal }); if (!control?.signal.aborted) { drafts.delete(`${projectID.value}:${selected.value.id}`); selected.value = null; selectionVersion++; thread.value = emptyChat(); deleteOpen.value = false; await reload() } }
 catch { if (!control?.signal.aborted) deleteError.value = t('chat.deleteError') }
 finally { deleting.value = false }
}
function startProject() {
 controller?.abort(); if (timer) clearInterval(timer)
 projectVersion++; selectionVersion++; reloadPromise = null; reloadAgain = false; onlineBusy = false; inboxPages = 1; wantedInboxPages = 1; moreLoading.value = false
 visitors.value = []; conversations.value = []; selected.value = null; thread.value = emptyChat(); nextCursor.value = ''; draft.value = ''; pending = null; enabled.value = false; error.value = ''; threadError.value = false; threadLoading.value = false; olderLoading.value = false
 if (!projectID.value || !canRead.value) return
 const control = new AbortController(); controller = control; state.value = 'connecting'
 void reload(); timer = setInterval(() => { void reloadOnline() }, 20_000)
 void watchSupport({ url: `${base()}/events`, token: () => session.value.accessToken, refreshAuth: $refreshSession, signal: control.signal, onChange: () => { void reload() }, onPresence: () => { void reloadOnline() }, onState: value => { if (control.signal.aborted) return; state.value = value; if (value === 'forbidden') { control.abort(); if (timer) clearInterval(timer); visitors.value = []; conversations.value = []; selected.value = null; thread.value = emptyChat() } } })
}
watch(projectID, startProject)
function onVisibility() { if (!document.hidden) void reload() }
onMounted(async () => {
 if (!canRead.value) { loadingProjects.value = false; return }
 document.addEventListener('visibilitychange', onVisibility)
 try { const data = await $apiFetch<{ projects: Project[] }>('/v1/projects'); projects.value = data.projects; const requested = String(route.query.project || ''); projectID.value = projects.value.find(project => project.id === requested)?.id || projects.value[0]?.id || '' }
 catch { error.value = t('chat.error') }
 finally { loadingProjects.value = false }
})
onBeforeUnmount(() => { controller?.abort(); if (timer) clearInterval(timer); document.removeEventListener('visibilitychange', onVisibility) })
</script>

<template>
 <section class="chat-console" data-fidibeki-block>
  <header class="chat-head"><div><h1>{{ t('chat.title') }}</h1><p>{{ t('chat.subtitle') }}</p></div><span class="chat-status" role="status" :data-live="state === 'connected'"><i />{{ connectionLabel }}</span></header>
  <p v-if="!canRead" role="alert">{{ t('chat.noAccess') }}</p>
  <template v-else>
   <div class="chat-project"><label for="chat-project">{{ t('chat.project') }}</label><select id="chat-project" v-model="projectID" :disabled="sending || settingsBusy || loadingProjects || deleteOpen"><option value="" disabled>{{ t('chat.chooseProject') }}</option><option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option></select><button v-if="projectID && permissionWrite('project:update')" type="button" class="app-button" :disabled="settingsBusy || state === 'forbidden'" @click="changeSetting">{{ settingLabel }}</button></div>
   <p v-if="loadingProjects" class="chat-hint">{{ t('chat.loading') }}</p><p v-else-if="!projects.length" class="chat-hint">{{ t('chat.noProjects') }}</p>
   <p v-if="projectID" class="chat-hint">{{ settingHint }}</p>
   <div v-if="error" class="chat-error" role="alert">{{ error }} <button type="button" @click="reload"><RefreshCw :size="14" />{{ t('chat.retry') }}</button></div>
   <div v-if="projectID && state !== 'forbidden'" class="chat-workspace" :data-selected="Boolean(selected)">
    <aside class="chat-directory"><nav class="chat-tabs" :aria-label="t('chat.lists')"><button type="button" :aria-pressed="view === 'online'" @click="view = 'online'"><Users :size="15" />{{ t('chat.online') }} <span>{{ visitors.length }}</span></button><button type="button" :aria-pressed="view === 'inbox'" @click="view = 'inbox'"><MessageCircle :size="15" />{{ t('chat.inbox') }} <span v-if="unread">{{ unread }}</span></button></nav>
     <div class="chat-search"><input ref="searchInput" v-model="search" type="search" :aria-label="t('chat.search')" :placeholder="t('chat.search')"></div>
     <div class="chat-people"><template v-if="view === 'online'"><p v-if="!filteredVisitors.length" class="chat-empty">{{ onlineEmpty }}</p><button v-for="visitor in filteredVisitors" :key="visitor.id" type="button" class="chat-person" :data-selected="selected?.id === visitor.id" :disabled="sending" @click="select(visitor)"><span class="chat-avatar" :data-active="visitor.state === 'active'"><Users :size="18" /></span><span class="chat-person-copy"><strong>{{ visitor.name }}</strong><small>{{ visitorState(visitor.state) }}</small><small>{{ visitor.page_url }}</small></span></button><p v-if="truncated" class="chat-empty">{{ t('chat.truncated') }}</p></template>
      <template v-else><p v-if="!filteredConversations.length" class="chat-empty">{{ inboxEmpty }}</p><button v-for="conversation in filteredConversations" :key="conversation.id" type="button" class="chat-person" :data-selected="selected?.id === conversation.id" :disabled="sending" @click="select(conversation)"><span class="chat-avatar"><MessageCircle :size="18" /></span><span class="chat-person-copy"><strong>{{ conversation.name }}</strong><small>{{ conversation.preview }}</small></span><b v-if="conversation.unread" class="chat-unread">{{ conversation.unread }}</b></button><button v-if="nextCursor" class="chat-link" type="button" :disabled="moreLoading" @click="more">{{ t('chat.more') }}</button></template>
     </div>
    </aside>
    <section class="chat-thread" :aria-label="t('chat.selected')"><div v-if="!selected" class="chat-welcome"><MessageCircle :size="34" /><h2>{{ t('chat.welcome') }}</h2><p>{{ t('chat.welcomeBody') }}</p><small>{{ t('chat.rule') }}</small></div>
     <template v-else><header class="chat-thread-head"><button class="chat-back app-icon-button" type="button" :aria-label="t('chat.back')" @click="back"><ArrowLeft :size="18" /></button><div><h2>{{ selected.name }}</h2><p>{{ liveVisitor ? visitorState(liveVisitor.state) : t('chat.offline') }}<span v-if="liveVisitor"> · {{ liveVisitor.page_url }}</span></p></div><button class="app-icon-button" type="button" :aria-label="t('chat.close')" @click="back"><X :size="18" /></button></header>
      <div v-if="threadError" class="chat-error" role="alert">{{ t('chat.threadError') }} <button type="button" @click="loadThread(true)">{{ t('chat.retry') }}</button></div>
      <div ref="history" class="chat-history" role="log" :aria-label="t('chat.messages')" aria-live="polite" @scroll="trackScroll"><p v-if="threadLoading" class="chat-empty">{{ t('chat.loading') }}</p><div v-else-if="!thread.messages.length && !threadError" class="chat-first"><h3>{{ t('chat.first') }}</h3><p>{{ t('chat.firstBody') }}</p><button v-if="canSend" class="app-button" type="button" @click="draft = t('chat.prompt')">{{ t('chat.ask') }}</button></div><button v-if="thread.has_more" type="button" class="chat-link" :disabled="olderLoading" @click="older">{{ t('chat.older') }}</button><article v-for="message in thread.messages" :key="message.id" class="chat-bubble" :data-own="message.sender_type === 'operator'"><div><strong>{{ message.sender_name }}</strong><time :datetime="message.created_at">{{ time(message.created_at) }}</time></div><p>{{ message.body }}</p></article></div>
      <div class="chat-actions"><button v-if="selected && permissionWrite('chat:delete')" type="button" class="chat-link" :disabled="sending" @click="deleteError = ''; deleteOpen = true">{{ t('chat.delete') }}</button><button v-if="thread.conversation" type="button" class="chat-link chat-download" :disabled="downloading" @click="download">{{ t('chat.download') }}</button></div>
      <form v-if="canSend" class="chat-compose" @submit.prevent="send"><label for="website-chat-message">{{ t('chat.message', { name: selected.name }) }}</label><textarea id="website-chat-message" ref="reply" v-model="draft" rows="3" maxlength="8000" :placeholder="t('chat.placeholder')" :disabled="sending || threadLoading || threadError" @keydown.ctrl.enter.prevent="send" @keydown.meta.enter.prevent="send" /><p v-if="sendError" class="chat-error" role="alert">{{ t('chat.sendError') }}</p><footer><small>{{ Array.from(draft.trim()).length.toLocaleString() }} / 4,000</small><button class="app-button app-button--primary" type="submit" :disabled="sending || threadLoading || threadError || !draft.trim() || Array.from(draft.trim()).length > 4000"><Send :size="15" />{{ sendLabel }}</button></footer></form><p v-else class="chat-hint">{{ replyUnavailable }}</p>
     </template>
    </section>
   </div>
  </template>
  <AppConfirmationDialog :open="deleteOpen" :title="t('chat.deleteTitle')" :description="t('chat.deleteDescription')" :confirm-label="t('chat.delete')" :pending-label="t('chat.deleting')" :cancel-label="t('common.cancel')" :pending="deleting" :error="deleteError" @cancel="deleteOpen = false" @confirm="removeSelected" />
 </section>
</template>

<style scoped>
.chat-actions{display:flex;justify-content:space-between;flex-direction:row-reverse;gap:12px;padding:0 12px}.chat-console{max-width:1440px;margin:0 auto;padding:24px}.chat-head{display:flex;justify-content:space-between;align-items:center;gap:16px;margin-bottom:22px}.chat-head h1{margin:0 0 6px;font-size:26px;letter-spacing:-.025em}.chat-head p{margin:0;color:var(--ink-soft)}.chat-status{display:inline-flex;align-items:center;gap:7px;color:var(--ink-soft);font-size:12px;white-space:nowrap}.chat-status i{width:7px;height:7px;border-radius:50%;background:var(--amber)}.chat-status[data-live=true] i{background:var(--teal)}.chat-project{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.chat-project label{font-size:12px;font-weight:650}.chat-project select{max-width:100%;min-width:200px;padding:9px 12px;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--ink);font:inherit}.chat-hint{color:var(--ink-soft);font-size:12px;margin:14px 0;line-height:1.6}.chat-workspace{display:grid;grid-template-columns:300px minmax(0,1fr);height:min(720px,calc(100dvh - 260px));min-height:420px;border:1px solid var(--line);border-radius:10px;overflow:hidden;background:var(--surface)}.chat-directory{display:flex;min-height:0;flex-direction:column;border-right:1px solid var(--line)}.chat-tabs{display:flex;gap:18px;padding:0 16px;border-bottom:1px solid var(--line)}.chat-tabs button{display:flex;gap:6px;align-items:center;padding:15px 0;border:0;background:transparent;color:var(--ink-soft);font:inherit;font-size:12px;font-weight:650}.chat-tabs button[aria-pressed=true]{color:var(--teal);box-shadow:0 2px var(--teal)}.chat-tabs span,.chat-unread{min-width:18px;border-radius:12px;padding:1px 5px;background:var(--teal-soft);color:var(--teal);font-size:10px;text-align:center}.chat-search{padding:14px}.chat-search input{width:100%;padding:9px 10px;border:1px solid var(--line);border-radius:6px;color:var(--ink);background:var(--surface);font:inherit;font-size:12px}.chat-people{flex:1;overflow:auto}.chat-empty{padding:20px;color:var(--ink-soft);font-size:12px;line-height:1.6}.chat-person{width:100%;display:flex;gap:10px;align-items:center;padding:14px;border:0;border-bottom:1px solid var(--line);background:transparent;text-align:left;color:var(--ink);font:inherit}.chat-person:hover,.chat-person[data-selected=true]{background:var(--teal-soft)}.chat-avatar{display:grid;place-items:center;flex:0 0 34px;height:34px;border-radius:50%;background:var(--surface-soft);color:var(--ink-soft)}.chat-avatar[data-active=true]{color:var(--teal);box-shadow:0 0 0 2px var(--teal-soft)}.chat-person-copy{display:flex;flex-direction:column;gap:4px;min-width:0;flex:1}.chat-person-copy strong{font-size:12px}.chat-person-copy small{font-size:11px;color:var(--ink-soft);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.chat-thread{display:flex;min-width:0;min-height:0;flex-direction:column}.chat-welcome{margin:auto;max-width:410px;padding:30px;text-align:center;color:var(--ink-soft)}.chat-welcome svg{display:block;margin:0 auto 16px;color:var(--teal)}.chat-welcome h2{color:var(--ink);font-size:20px}.chat-welcome p,.chat-first p{font-size:13px;line-height:1.7}.chat-welcome small{font-size:11px}.chat-thread-head{display:flex;align-items:center;gap:12px;padding:18px;border-bottom:1px solid var(--line)}.chat-thread-head>div{flex:1;min-width:0}.chat-thread-head h2{font-size:16px;margin:0 0 5px}.chat-thread-head p{font-size:11px;color:var(--ink-soft);margin:0;overflow-wrap:anywhere}.chat-back{display:none}.chat-history{flex:1;min-height:0;overflow:auto;display:flex;flex-direction:column;gap:18px;padding:24px;overscroll-behavior:contain}.chat-first{margin:auto;color:var(--ink-soft);max-width:400px}.chat-first h3{color:var(--ink);font-size:18px}.chat-bubble{max-width:85%;align-self:flex-start}.chat-bubble[data-own=true]{align-self:flex-end}.chat-bubble>div{display:flex;gap:12px;align-items:center;font-size:10px;color:var(--ink-soft)}.chat-bubble strong{font-weight:650}.chat-bubble p{margin:6px 0 0;padding:12px 14px;border-radius:7px;background:var(--surface-soft);font-size:13px;line-height:1.6;white-space:pre-wrap;overflow-wrap:anywhere}.chat-bubble[data-own=true] p{background:var(--teal-soft)}.chat-compose{padding:16px 20px;border-top:1px solid var(--line)}.chat-compose label{display:block;font-size:11px;font-weight:650;margin-bottom:8px}.chat-compose textarea{width:100%;min-height:70px;max-height:180px;padding:11px;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--ink);resize:vertical;font:inherit;font-size:13px}.chat-compose footer{display:flex;justify-content:space-between;align-items:center;margin-top:9px;gap:12px}.chat-compose small{font-size:10px;color:var(--ink-soft)}.chat-link{border:0;background:transparent;color:var(--teal);font:inherit;font-size:12px;text-decoration:underline;padding:10px;cursor:pointer}.chat-download{align-self:flex-start;margin-left:12px}.chat-error{padding:12px 16px;font-size:12px;color:var(--red);background:var(--surface-soft)}.chat-error button{display:inline-flex;gap:5px;align-items:center;color:inherit;border:0;background:none;text-decoration:underline;cursor:pointer}.chat-thread>.chat-hint{padding:0 20px}
@media(max-width:760px){.chat-console{padding:18px 12px}.chat-head h1{font-size:23px}.chat-workspace{grid-template-columns:1fr;height:calc(100dvh - 320px);min-height:420px}.chat-workspace:not([data-selected=true]) .chat-thread{display:none}.chat-workspace[data-selected=true] .chat-directory{display:none}.chat-directory{border-right:0}.chat-back{display:inline-flex}.chat-history{padding:16px}.chat-compose{padding:12px}.chat-bubble{max-width:95%}.chat-project select{min-width:0;flex:1}.chat-project label{width:100%}}
</style>
