<!--
THESIS: A personal conversation stays beside the user's work and never interrupts it.
OWN-WORLD: Existing Avenir typography, teal actions, solid surfaces, and six-pixel corners.
STORY: Notice a message, read it, reply, or give structured feedback from one launcher.
FIRST VIEWPORT: A bottom-right launcher; a quiet preview only for unread staff messages.
FORM: Scoped extension of the app shell. A compact conversation panel and two explicit tabs.
-->
<script setup lang="ts">
import { MessageCircle, Send, X } from '@lucide/vue'

const { t } = useI18n()
const { dateTime } = useRegionalFormat()
const formatTime = (value: string) => dateTime(value, { dateStyle: 'short', timeStyle: 'short' })
const runtime = useRuntimeConfig()
const { thread, state, loading, loadError, sending, sendError, olderLoading, open, tab, draft, unread, preview,
  reload, send, loadOlder, exporting, exportError, downloadTranscript, dismissPreview } = useLiveSupport()
const launcher = ref<HTMLButtonElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const messages = ref<HTMLElement | null>(null)
const feedbackTarget = ref<HTMLElement | null>(null)
const feedbackState = ref<'loading' | 'ready' | 'unavailable'>('loading')
let feedbackUnmount: (() => void) | null = null
let feedbackAttempt = 0
let nearBottom = true
let feedbackTimer: ReturnType<typeof setTimeout> | null = null
const feedbackConfigured = computed(() => Boolean(runtime.public.widgetUrl && runtime.public.widgetKey))
type FeedbackAPI = { mount(target: HTMLElement): Promise<{ mounted: boolean; unmount(): void }> }

async function mountFeedback() {
  if (!open.value || tab.value !== 'feedback' || !feedbackTarget.value) return
  const attempt = ++feedbackAttempt
  feedbackUnmount?.(); feedbackUnmount = null
  feedbackState.value = 'loading'
  if (!feedbackConfigured.value) { feedbackState.value = 'unavailable'; return }
  const api = (window as unknown as { Fidibeki?: { feedback?: FeedbackAPI } }).Fidibeki?.feedback
  if (!api) {
    if (feedbackTimer) clearTimeout(feedbackTimer)
    feedbackTimer = setTimeout(() => { if (feedbackState.value === 'loading') feedbackState.value = 'unavailable' }, 10_000)
    return
  }
  try {
    const result = await api.mount(feedbackTarget.value)
    if (attempt !== feedbackAttempt) { result.unmount(); return }
    feedbackUnmount = result.unmount
    feedbackState.value = result.mounted ? 'ready' : 'unavailable'
  } catch { feedbackState.value = 'unavailable' }
}
function close() { open.value = false; void nextTick(() => launcher.value?.focus()) }
function trackScroll() { const el = messages.value; nearBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 80 }
function onEscape(event: KeyboardEvent) { if (event.key === 'Escape' && open.value) { event.stopPropagation(); close() } }
watch(open, async value => {
  if (value) { await nextTick(); closeButton.value?.focus(); nearBottom = true; messages.value?.scrollTo({ top: messages.value.scrollHeight }) }
})
watch([open, tab], async () => {
  feedbackAttempt++; feedbackUnmount?.(); feedbackUnmount = null
  if (open.value && tab.value === 'feedback') { await nextTick(); await mountFeedback() }
})
watch(messages, (element, _previous, cleanup) => {
  if (!element) return
  const observer = new ResizeObserver(() => { if (nearBottom) element.scrollTop = element.scrollHeight })
  observer.observe(element); cleanup(() => observer.disconnect())
})
watch(() => thread.value.messages.at(-1)?.id, async () => {
  if (!nearBottom) return
  await nextTick(); messages.value?.scrollTo({ top: messages.value.scrollHeight })
})
onMounted(() => {
  window.addEventListener('fidibeki:ready', mountFeedback)
  mountFeedbackWidget(document, String(runtime.public.widgetUrl || ''), String(runtime.public.widgetKey || ''), true)
})
onBeforeUnmount(() => {
  feedbackAttempt++; feedbackUnmount?.()
  if (feedbackTimer) clearTimeout(feedbackTimer)
  window.removeEventListener('fidibeki:ready', mountFeedback)
})
</script>

<template>
  <div class="support-root rr-block" data-fidibeki-block @keydown="onEscape">
    <aside v-if="preview" class="support-preview" :aria-label="t('support.newMessage')">
      <button class="support-preview-dismiss" type="button" :aria-label="t('support.dismiss')" @click="dismissPreview"><X :size="16" /></button>
      <button class="support-preview-message" type="button" @click="tab = 'messages'; open = true">
        <strong>{{ t('support.sender', { name: preview.sender_name }) }}</strong><span>{{ preview.body }}</span><small>{{ t('support.readAndReply') }}</small>
      </button>
    </aside>
    <section v-if="open" id="fidibeki-support" class="support-panel" role="dialog" :aria-label="t('support.title')">
      <header class="support-head">
        <div><strong>{{ t('support.title') }}</strong><p>{{ t('support.subtitle') }}</p></div>
        <button ref="closeButton" type="button" class="support-icon" :aria-label="t('support.close')" @click="close"><X :size="18" /></button>
      </header>
      <nav class="support-tabs" :aria-label="t('support.sections')">
        <button type="button" :aria-pressed="tab === 'messages'" @click="tab = 'messages'">{{ t('support.messages') }}<span v-if="unread">{{ unread }}</span></button>
        <button type="button" :aria-pressed="tab === 'feedback'" @click="tab = 'feedback'">{{ t('support.feedback') }}</button>
      </nav>
      <template v-if="tab === 'messages'">
        <p v-if="state === 'offline'" class="support-notice" role="status">{{ t('support.reconnecting') }}</p>
        <p v-if="state === 'forbidden'" class="support-notice" role="alert">{{ t('support.unavailable') }}</p>
        <div v-if="loadError" class="support-notice" role="alert">{{ t('support.loadError') }} <button type="button" @click="reload">{{ t('support.retry') }}</button></div>
        <div ref="messages" class="support-messages" role="log" :aria-label="t('support.messages')" aria-live="polite" @scroll="trackScroll">
          <p v-if="loading" class="support-empty">{{ t('support.loading') }}</p>
          <div v-else-if="!thread.conversation && !loadError" class="support-empty"><MessageCircle :size="28" /><h2>{{ t('support.emptyTitle') }}</h2><p>{{ t('support.emptyBody') }}</p><button type="button" class="app-button" @click="tab = 'feedback'">{{ t('support.feedback') }}</button></div>
          <button v-if="thread.has_more" class="support-older" type="button" :disabled="olderLoading" @click="loadOlder">{{ t('support.older') }}</button>
          <article v-for="message in thread.messages" :key="message.id" class="support-message" :data-own="message.sender_type === 'member'">
            <span class="support-message-author">{{ message.sender_type === 'member' ? t('support.you') : message.sender_name }}</span>
            <p>{{ message.body }}</p>
            <time :datetime="message.created_at">{{ formatTime(message.created_at) }}</time>
          </article>
        </div>
        <div v-if="thread.conversation" class="support-export"><button type="button" :disabled="exporting" @click="downloadTranscript">{{ exporting ? t('support.exporting') : t('support.export') }}</button><p v-if="exportError" role="alert">{{ t('support.exportError') }}</p></div>
        <form v-if="thread.conversation && state !== 'forbidden'" class="support-compose" @submit.prevent="send">
          <label class="sr-only" for="support-reply">{{ t('support.reply') }}</label>
          <textarea id="support-reply" v-model="draft" rows="3" maxlength="8000" :placeholder="t('support.replyPlaceholder')" :disabled="sending" @keydown.ctrl.enter.prevent="send" @keydown.meta.enter.prevent="send" />
          <p v-if="sendError" class="support-send-error" role="alert">{{ t('support.sendError') }}</p>
          <div><small>{{ t('support.replyHint') }}</small><button class="app-button app-button--primary" type="submit" :disabled="sending || !draft.trim() || Array.from(draft.trim()).length > 4000"><Send :size="14" />{{ sending ? t('support.sending') : t('support.send') }}</button></div>
          <small v-if="Array.from(draft.trim()).length > 4000" role="alert">{{ t('support.tooLong') }}</small>
        </form>
      </template>
      <div v-else class="support-feedback">
        <p v-if="feedbackState === 'loading'" role="status">{{ t('support.loading') }}</p>
        <p v-if="feedbackState === 'unavailable'" role="status">{{ t('support.feedbackUnavailable') }} <button v-if="feedbackConfigured" type="button" @click="mountFeedback">{{ t('support.retry') }}</button></p>
        <div ref="feedbackTarget" />
      </div>
    </section>
    <button ref="launcher" type="button" class="support-launcher" aria-controls="fidibeki-support" :aria-expanded="open" @click="open ? close() : open = true">
      <MessageCircle :size="20" /><span>{{ t('support.launcher') }}</span><span v-if="unread" class="support-badge" :aria-label="t('support.unread', { count: unread })">{{ unread > 99 ? '99+' : unread }}</span>
    </button>
  </div>
</template>

<style scoped>
.support-root { position: fixed; right: 20px; bottom: 20px; z-index: 90; font-size: 14px; }
.support-launcher { display: flex; align-items: center; gap: 9px; margin-left: auto; min-height: 46px; padding: 0 16px; border: 0; border-radius: var(--radius); background: var(--teal-fill); color: var(--on-accent); font: inherit; font-weight: 650; cursor: pointer; }
.support-launcher:hover { background: var(--teal-fill-strong); }
.support-badge { display: grid; min-width: 22px; height: 22px; padding: 0 5px; place-items: center; border-radius: 20px; background: var(--on-accent); color: var(--teal-fill); font-size: 12px; }
.support-panel { display: flex; flex-direction: column; width: 380px; height: min(620px, calc(100dvh - 100px)); margin-bottom: 12px; border-radius: 10px; background: var(--surface); color: var(--ink); box-shadow: var(--shadow-overlay); overflow: hidden; }
.support-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 20px 20px 14px; }
.support-head strong { font-size: 17px; }
.support-head p { margin: 4px 0 0; color: var(--ink-soft); font-size: 12px; }
.support-icon, .support-preview-dismiss { display: grid; flex: 0 0 36px; width: 36px; height: 36px; place-items: center; border: 0; background: transparent; color: var(--ink); border-radius: var(--radius); cursor: pointer; }
.support-icon:hover, .support-preview-dismiss:hover { background: var(--surface-soft); }
.support-tabs { display: flex; border-bottom: 1px solid var(--line); padding: 0 20px; gap: 24px; }
.support-tabs button { position: relative; display: flex; align-items: center; gap: 6px; padding: 10px 0; border: 0; background: transparent; color: var(--ink-soft); font: inherit; font-weight: 600; cursor: pointer; }
.support-tabs button[aria-pressed='true'] { color: var(--teal); box-shadow: 0 2px 0 var(--teal); }
.support-tabs span { font-size: 11px; }
.support-messages { display: flex; flex-direction: column; gap: 16px; min-height: 0; flex: 1; overflow-y: auto; overscroll-behavior: contain; padding: 20px; }
.support-empty { margin: auto; text-align: center; color: var(--ink-soft); line-height: 1.6; }
.support-empty h2 { margin: 12px 0 8px; font-size: 17px; color: var(--ink); }
.support-empty p { margin: 0 0 20px; }
.support-message { max-width: 90%; align-self: flex-start; }
.support-message[data-own='true'] { align-self: flex-end; }
.support-message-author { display: block; margin-bottom: 5px; font-size: 11px; font-weight: 650; color: var(--ink-soft); }
.support-message p { margin: 0; padding: 11px 13px; border-radius: 6px; background: var(--surface-soft); white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.55; }
.support-message[data-own='true'] p { background: var(--teal-soft); }
.support-message time { display: block; margin-top: 5px; color: var(--ink-soft); font-size: 10px; }
.support-export { padding: 0 16px 10px; font-size: 11px; color: var(--ink-soft); }
.support-export button { border: 0; background: transparent; color: var(--teal); text-decoration: underline; cursor: pointer; }
.support-compose { padding: 12px 16px 16px; border-top: 1px solid var(--line); }
.support-compose textarea { width: 100%; min-height: 64px; max-height: 130px; resize: vertical; font: inherit; padding: 10px; color: var(--ink); background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius); }
.support-compose textarea::placeholder { color: var(--ink-soft); }
.support-compose > div { display: flex; gap: 12px; align-items: center; justify-content: space-between; margin-top: 8px; }
.support-compose small { font-size: 11px; color: var(--ink-soft); }
.support-send-error { color: var(--red); margin: 8px 0; font-size: 12px; }
.support-notice { padding: 10px 16px; margin: 0; background: var(--surface-soft); color: var(--ink-soft); font-size: 12px; }
.support-notice button, .support-feedback button, .support-older { border: 0; background: transparent; color: var(--teal); text-decoration: underline; cursor: pointer; }
.support-feedback { flex: 1; min-height: 0; overflow-y: auto; padding: 16px; }
.support-preview { position: relative; width: 310px; margin: 0 0 12px auto; background: var(--surface); box-shadow: var(--shadow-overlay); border-radius: 10px; }
.support-preview-dismiss { position: absolute; right: 5px; top: 5px; }
.support-preview-message { display: block; width: 100%; padding: 18px; border: 0; background: transparent; color: var(--ink); text-align: left; font: inherit; cursor: pointer; }
.support-preview-message strong { display: block; padding-right: 24px; font-size: 12px; }
.support-preview-message span { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; margin: 8px 0 10px; line-height: 1.5; overflow-wrap: anywhere; }
.support-preview-message small { color: var(--teal); font-weight: 650; }
button:focus-visible, textarea:focus-visible { outline: 2px solid var(--teal); outline-offset: 3px; }
@media (max-width: 480px) {
  .support-root { right: 12px; bottom: max(12px, env(safe-area-inset-bottom)); left: 12px; }
  .support-panel { width: 100%; height: min(620px, calc(100dvh - 92px)); }
  .support-preview { max-width: 100%; }
}
</style>
