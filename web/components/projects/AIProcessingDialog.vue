<script setup lang="ts">
import { ShieldCheck, X } from '@lucide/vue'
import type { AIInputCategory, AIProcessingConfig } from '~/types/api'

const props = defineProps<{
  open: boolean
  initial: AIProcessingConfig
  defaultPrivacyUrl?: string | null
  screenshot: boolean
  audio: boolean
  video: boolean
}>()
const emit = defineEmits<{
  cancel: []
  confirm: [configuration: AIProcessingConfig]
}>()
const { t } = useI18n()
const identifier = useId()
const privacyURL = ref('')
const categories = ref<AIInputCategory[]>([])
const acknowledged = ref(false)

watch(() => props.open, (open) => {
  if (!open) return
  privacyURL.value = props.initial.privacy_policy_url || props.defaultPrivacyUrl || ''
  categories.value = props.initial.enabled
    ? [...(props.initial.input_categories || [])]
    : ['feedback_text']
  categories.value = categories.value.filter(category => categoryAvailable(category))
  if (categories.value.length === 0) categories.value = ['feedback_text']
  acknowledged.value = false
}, { immediate: true })

const valid = computed(() =>
  categories.value.length > 0 && acknowledged.value && validPrivacyURL(privacyURL.value),
)

function categoryAvailable(category: AIInputCategory) {
  if (category === 'feedback_text') return true
  if (category === 'screenshot') return props.screenshot
  if (category === 'audio') return props.audio
  return props.video
}

function toggleCategory(category: AIInputCategory, checked: boolean) {
  categories.value = checked
    ? [...new Set([...categories.value, category])]
    : categories.value.filter(value => value !== category)
}

function validPrivacyURL(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password
  } catch {
    return false
  }
}

function confirm() {
  if (!valid.value) return
  emit('confirm', {
    enabled: true,
    policy_version: '2026-07-24',
    provider: 'google_gemini',
    purposes: ['transcription', 'cleanup', 'moderation', 'sentiment', 'triage_recommendations'],
    input_categories: [...categories.value],
    privacy_policy_url: privacyURL.value.trim(),
    sensitive_data_mode: 'prohibit_and_quarantine',
    acknowledged: true,
  })
}
</script>

<template>
  <AppDialog
    :open="open"
    initial-focus="[data-ai-category]"
    :aria-labelledby="`${identifier}-title`"
    @close="emit('cancel')"
  >
    <section class="app-modal ai-processing-dialog">
      <header>
        <div>
          <p class="app-eyebrow">{{ t('projects.editor.ai.eyebrow') }}</p>
          <h2 :id="`${identifier}-title`">{{ t('projects.editor.ai.dialogTitle') }}</h2>
        </div>
        <button class="app-icon-button" type="button" :title="t('common.cancel')" :aria-label="t('common.cancel')" @click="emit('cancel')">
          <X :size="16" />
        </button>
      </header>

      <div class="app-modal__body ai-processing-body">
        <div class="ai-processing-note" role="note">
          <ShieldCheck :size="18" />
          <p><b>{{ t('projects.editor.ai.providerTitle') }}</b><span>{{ t('projects.editor.ai.providerDescription') }}</span></p>
        </div>

        <fieldset>
          <legend>{{ t('projects.editor.ai.inputTitle') }}</legend>
          <label><input data-ai-category type="checkbox" :checked="categories.includes('feedback_text')" @change="toggleCategory('feedback_text', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.feedback_text') }}</label>
          <label v-if="screenshot"><input type="checkbox" :checked="categories.includes('screenshot')" @change="toggleCategory('screenshot', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.screenshot') }}</label>
          <label v-if="audio"><input type="checkbox" :checked="categories.includes('audio')" @change="toggleCategory('audio', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.audio') }}</label>
          <label v-if="video"><input type="checkbox" :checked="categories.includes('video')" @change="toggleCategory('video', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.video') }}</label>
        </fieldset>

        <label>
          {{ t('projects.editor.ai.privacyURL') }}
          <input v-model.trim="privacyURL" class="app-input" type="url" inputmode="url" placeholder="https://example.com/privacy" required />
        </label>

        <div class="ai-processing-policy">
          <p><b>{{ t('projects.editor.ai.policyTitle') }}</b><span>{{ t('projects.editor.ai.policyDescription') }}</span></p>
          <p><b>{{ t('projects.editor.ai.advisoryTitle') }}</b><span>{{ t('projects.editor.ai.advisoryDescription') }}</span></p>
        </div>

        <label class="ai-processing-acknowledgment">
          <input v-model="acknowledged" type="checkbox" />
          <span>{{ t('projects.editor.ai.acknowledgment') }}</span>
        </label>
      </div>

      <footer class="app-modal__actions">
        <button class="app-button" type="button" @click="emit('cancel')">{{ t('common.cancel') }}</button>
        <button class="app-button app-button--primary" type="button" :disabled="!valid" @click="confirm">{{ t('projects.editor.ai.enable') }}</button>
      </footer>
    </section>
  </AppDialog>
</template>

<style scoped>
.ai-processing-dialog { width: min(620px, calc(100vw - 24px)); }
.ai-processing-dialog > header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; padding: 20px 22px 16px; border-bottom: 1px solid var(--line); }
.ai-processing-dialog h2 { margin: 3px 0 0; font-size: 18px; letter-spacing: 0; }
.ai-processing-body { display: grid; gap: 18px; }
.ai-processing-note { display: grid; grid-template-columns: 20px minmax(0, 1fr); gap: 10px; padding: 12px; border: 1px solid var(--line); border-radius: 6px; }
.ai-processing-note svg { color: var(--green); }
.ai-processing-note p, .ai-processing-policy p { display: grid; gap: 3px; margin: 0; }
.ai-processing-note span, .ai-processing-policy span { color: var(--muted); font-size: 12px; line-height: 1.5; }
.ai-processing-body fieldset { display: flex; flex-wrap: wrap; gap: 10px 18px; margin: 0; padding: 0; border: 0; }
.ai-processing-body legend { width: 100%; margin-bottom: 8px; font-weight: 700; }
.ai-processing-body fieldset label, .ai-processing-acknowledgment { display: flex; align-items: flex-start; gap: 8px; }
.ai-processing-policy { display: grid; gap: 12px; padding-top: 14px; border-top: 1px solid var(--line); }
.ai-processing-acknowledgment { padding: 12px; background: #f6f8f7; border: 1px solid var(--line); border-radius: 6px; font-size: 12px; line-height: 1.5; }
@media (max-width: 560px) {
  .ai-processing-dialog { width: calc(100vw - 16px); max-height: calc(100dvh - 16px); }
  .ai-processing-body fieldset { display: grid; }
}
</style>
