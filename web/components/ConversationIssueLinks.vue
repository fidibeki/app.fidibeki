<script setup lang="ts">
import { Link, Unlink, X } from '@lucide/vue'
import type { ChatMessage } from '../../shared/website-chat'
import type { Issue } from '~/types/api'
import type { ConversationIssue } from '~/types/conversation-issues'
import { issueStatusKey } from '~/utils/issue-activity'

const props = defineProps<{ projectId: string; visitorId: string }>()
const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t } = useI18n()
const readable = computed(() => can('chat:read') && can('issue:read') && can('project:read'))
const writable = computed(() => readable.value && canWrite('chat:write') && canWrite('issue:update'))
const links = ref<ConversationIssue[]>([])
const summaries = computed(() => [...new Map(links.value.map(link => [link.id, link])).values()])
const truncated = ref(false)
const loadError = ref(false)
const message = ref<ChatMessage | null>(null)
const mode = ref<'create' | 'link'>('create')
const title = ref('')
const description = ref('')
const issueId = ref('')
const candidates = ref<Issue[]>([])
const candidatesLoading = ref(false)
const candidatesError = ref(false)
const pending = ref(false)
const error = ref('')
const control = new AbortController()
const current = computed(() => links.value.find(link => link.message_id === message.value?.id))
const path = `/v1/projects/${props.projectId}/chat/visitors/${props.visitorId}/issues`
let refreshVersion = 0
function statusLabel(status: string) { const key = issueStatusKey(status); return key ? t(key) : status }
async function refresh() {
  if (!readable.value) return
  const version = ++refreshVersion
  try {
    const data = await $apiFetch<{ issues: ConversationIssue[]; truncated: boolean }>(path, { signal: control.signal })
    if (!control.signal.aborted && version === refreshVersion) { links.value = data.issues || []; truncated.value = data.truncated; loadError.value = false }
  } catch { if (!control.signal.aborted && version === refreshVersion) loadError.value = true }
}
async function loadCandidates() {
  candidatesLoading.value = true; candidatesError.value = false
  try {
    const data = await $apiFetch<{ issues: Issue[] }>('/v1/issues', { signal: control.signal })
    if (!control.signal.aborted) candidates.value = data.issues.filter(issue => issue.project_id === props.projectId && !issue.merged_into_id)
  } catch { if (!control.signal.aborted) candidatesError.value = true }
  finally { if (!control.signal.aborted) candidatesLoading.value = false }
}
function open(value: ChatMessage) {
  if (!writable.value || pending.value) return
  message.value = value; mode.value = 'create'; title.value = ''; description.value = ''; issueId.value = ''; error.value = ''
  void refresh()
}
function close() { if (!pending.value) message.value = null }
watch(mode, value => { if (value === 'link') void loadCandidates() })
async function save() {
  if (!message.value || !writable.value || pending.value) return
  if (mode.value === 'create' && (!title.value.trim() || Array.from(title.value.trim()).length > 80 || Array.from(description.value.trim()).length > 10000)) return
  if (mode.value === 'link' && !issueId.value) return
  pending.value = true; error.value = ''
  try {
    const body = mode.value === 'create'
      ? { message_id: message.value.id, title: title.value.trim(), description: description.value.trim() }
      : { message_id: message.value.id, issue_id: issueId.value }
    await $apiFetch(path, { method: 'POST', body, signal: control.signal })
    await refresh()
    if (!control.signal.aborted) message.value = null
  } catch { if (!control.signal.aborted) { error.value = t('chat.issues.saveError'); await refresh() } }
  finally { if (!control.signal.aborted) pending.value = false }
}
async function unlink() {
  if (!current.value || !writable.value || pending.value) return
  pending.value = true; error.value = ''
  try {
    await $apiFetch(`${path}/${current.value.message_id}`, { method: 'DELETE', signal: control.signal })
    await refresh()
    if (!control.signal.aborted) message.value = null
  } catch { if (!control.signal.aborted) error.value = t('chat.issues.unlinkError') }
  finally { if (!control.signal.aborted) pending.value = false }
}
onMounted(refresh)
onBeforeUnmount(() => control.abort())
defineExpose({ open, refresh })
</script>

<template>
  <section v-if="readable && (links.length || loadError)" class="conversation-issues" :aria-label="t('chat.issues.linked')">
    <h3>{{ t('chat.issues.linked') }}</h3>
    <p v-if="loadError" role="alert">{{ t('chat.issues.loadError') }} <button type="button" @click="refresh">{{ t('chat.retry') }}</button></p>
    <NuxtLink v-for="link in summaries" :key="link.id" :to="`/issues/${link.id}`"><Link :size="14" aria-hidden="true" /><span><b>{{ link.public_id }}</b> {{ link.title }}</span><small>{{ link.merged_into_id ? t('chat.issues.merged') : statusLabel(link.status) }}</small></NuxtLink>
    <p v-if="truncated">{{ t('chat.issues.truncated') }}</p>
  </section>
  <AppDialog :open="Boolean(message)" initial-focus="#conversation-issue-title" :aria-label="t('chat.issues.track')" @close="close">
    <form class="conversation-issue-form" @submit.prevent="save">
      <header><h2>{{ t('chat.issues.track') }}</h2><button type="button" class="app-icon-button" :aria-label="t('common.close')" :disabled="pending" @click="close"><X :size="18" /></button></header>
      <blockquote data-fidibeki-mask>{{ message?.body }}</blockquote>
      <p class="conversation-issue-hint">{{ t('chat.issues.privateHint') }}</p>
      <template v-if="current">
        <NuxtLink class="conversation-issue-current" :to="`/issues/${current.id}`">{{ current.public_id }} · {{ current.title }}</NuxtLink>
        <p>{{ t('chat.issues.unlinkHint') }}</p>
        <button type="button" class="app-button" :disabled="pending" @click="unlink"><Unlink :size="14" />{{ t('chat.issues.unlink') }}</button>
      </template>
      <template v-else>
        <fieldset :disabled="pending" class="conversation-issue-mode"><legend class="sr-only">{{ t('chat.issues.action') }}</legend><label><input v-model="mode" type="radio" value="create">{{ t('chat.issues.create') }}</label><label><input v-model="mode" type="radio" value="link">{{ t('chat.issues.existing') }}</label></fieldset>
        <template v-if="mode === 'create'">
          <label for="conversation-issue-title">{{ t('chat.issues.title') }}</label><input id="conversation-issue-title" v-model="title" maxlength="80" required :disabled="pending">
          <label for="conversation-issue-description">{{ t('chat.issues.description') }}</label><textarea id="conversation-issue-description" v-model="description" rows="3" maxlength="10000" :disabled="pending" />
          <p class="conversation-issue-hint">{{ t('chat.issues.summaryHint') }}</p>
        </template>
        <template v-else>
          <label for="conversation-issue-existing">{{ t('chat.issues.choose') }}</label><select id="conversation-issue-existing" v-model="issueId" required :disabled="pending || candidatesLoading || candidatesError"><option value="" disabled>{{ t(candidatesLoading ? 'chat.loading' : 'chat.issues.choose') }}</option><option v-for="candidate in candidates" :key="candidate.id" :value="candidate.id">{{ candidate.public_id }} · {{ candidate.title }}</option></select>
          <p v-if="candidatesError" role="alert">{{ t('chat.issues.loadError') }} <button type="button" @click="loadCandidates">{{ t('chat.retry') }}</button></p>
          <p v-else-if="!candidatesLoading && !candidates.length">{{ t('chat.issues.noCandidates') }}</p>
        </template>
        <footer><button type="button" class="app-button" :disabled="pending" @click="close">{{ t('common.cancel') }}</button><button type="submit" class="app-button app-button--primary" :disabled="pending || (mode === 'create' ? !title.trim() : !issueId || candidatesLoading || candidatesError)">{{ t(pending ? 'chat.issues.saving' : mode === 'create' ? 'chat.issues.create' : 'chat.issues.link') }}</button></footer>
      </template>
      <p v-if="error" class="conversation-issue-error" role="alert">{{ error }}</p>
    </form>
  </AppDialog>
</template>

<style scoped>
.conversation-issues{font-size:12px;display:grid;gap:8px;padding-bottom:12px}.conversation-issues h3{font-size:12px;margin:0;color:var(--ink-soft);font-weight:600}.conversation-issues>a{display:flex;gap:8px;align-items:baseline;color:var(--ink);text-decoration:none;overflow-wrap:anywhere}.conversation-issues>a:hover span{text-decoration:underline}.conversation-issues svg{flex-shrink:0;align-self:center}.conversation-issues span{flex:1;min-width:0}.conversation-issues b{font-weight:600;color:var(--teal)}.conversation-issues small{color:var(--ink-soft);white-space:nowrap}.conversation-issue-form{width:min(100%,560px);max-height:100%;overflow:auto;background:var(--surface);color:var(--ink);border-radius:12px;padding:24px;box-shadow:var(--shadow-lg,0 16px 48px #0003);display:grid;gap:12px}.conversation-issue-form header{display:flex;align-items:center;justify-content:space-between;gap:16px}.conversation-issue-form h2{margin:0;font-size:20px}.conversation-issue-form blockquote{margin:4px 0;padding:12px;background:var(--surface-soft);border-radius:6px;font-size:13px;line-height:1.6;max-height:130px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere}.conversation-issue-form p{margin:0;font-size:12px;line-height:1.6}.conversation-issue-hint{color:var(--ink-soft)}.conversation-issue-mode{display:flex;flex-wrap:wrap;gap:16px;border:0;padding:8px 0;margin:0}.conversation-issue-mode label{display:flex;gap:6px;align-items:center;font-size:13px}.conversation-issue-form>label{font-size:12px;font-weight:600;margin-top:8px}.conversation-issue-form>input,.conversation-issue-form>textarea,.conversation-issue-form>select{width:100%;min-width:0;background:var(--surface);color:var(--ink);border:1px solid var(--line);border-radius:6px;padding:10px;font:inherit;font-size:14px}.conversation-issue-form textarea{resize:vertical}.conversation-issue-form footer{display:flex;justify-content:flex-end;flex-wrap:wrap;gap:8px;margin-top:8px}.conversation-issue-current{color:var(--teal);overflow-wrap:anywhere}.conversation-issue-error{color:var(--danger)}@media(max-width:600px){.conversation-issue-form{padding:20px 16px}.conversation-issues>a{flex-wrap:wrap}.conversation-issues small{margin-left:22px}}
</style>
