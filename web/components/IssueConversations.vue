<script setup lang="ts">
import { ArrowUpRight, MessageCircle } from '@lucide/vue'
import type { ChatConversation } from '../../shared/website-chat'
const props = defineProps<{ projectId: string; issueId: string }>()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const conversations = ref<ChatConversation[]>([])
const failed = ref(false)
const truncated = ref(false)
const control = new AbortController()
async function load() {
  try {
    const data = await $apiFetch<{ conversations: ChatConversation[]; truncated: boolean }>(`/v1/projects/${props.projectId}/chat/issues/${props.issueId}/conversations`, { signal: control.signal })
    if (!control.signal.aborted) { conversations.value = data.conversations || []; truncated.value = data.truncated; failed.value = false }
  } catch { if (!control.signal.aborted) failed.value = true }
}
onMounted(load)
onBeforeUnmount(() => control.abort())
</script>

<template>
  <section v-if="conversations.length || failed" class="app-surface issue-conversations" :aria-label="t('chat.issues.sources')">
    <h2><MessageCircle :size="16" aria-hidden="true" />{{ t('chat.issues.sources') }}</h2>
    <p>{{ t('chat.issues.replyHint') }}</p>
    <p v-if="failed" role="alert">{{ t('chat.issues.loadError') }} <button type="button" class="app-button" @click="load">{{ t('chat.retry') }}</button></p>
    <NuxtLink v-for="conversation in conversations" :key="conversation.id" :to="{ path: '/conversations', query: { project: projectId, visitor: conversation.id } }"><span>{{ conversation.name }}<small>{{ t('chat.issues.return') }}</small></span><ArrowUpRight :size="16" aria-hidden="true" /></NuxtLink>
    <p v-if="truncated">{{ t('chat.issues.truncated') }}</p>
  </section>
</template>

<style scoped>
.issue-conversations{padding:20px}.issue-conversations h2{display:flex;align-items:center;gap:8px;font-size:15px;margin:0 0 8px}.issue-conversations p{font-size:12px;line-height:1.6;color:var(--ink-soft);margin:0 0 12px}.issue-conversations a{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:52px;color:var(--ink);text-decoration:none}.issue-conversations a:hover{color:var(--teal)}.issue-conversations span{min-width:0;overflow-wrap:anywhere;font-size:13px}.issue-conversations small{display:block;font-size:11px;margin-top:4px;color:var(--ink-soft)}
</style>
