<script setup lang="ts">
import { ShieldCheck, X } from '@lucide/vue'
import type { AIInputCategory, AIProcessingConfig, AIConnection, AIProviderCatalog } from '~/types/api'

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
const { $apiFetch } = useNuxtApp()
const { can } = useAuth()
const localizedError = useLocalizedAPIError()
const identifier = useId()
const connections = ref<AIConnection[]>([])
const catalog = ref<AIProviderCatalog | null>(null)
const connectionID = ref('managed')
const modelID = ref('')
const loadError = ref('')
const loading = ref(false)
const connection = computed(() => connections.value.find(c => c.id === connectionID.value))
const models = computed(() => connection.value?.models?.filter(m => m.validated) || [])
const selectedModel = computed(() => models.value.find(m => m.id === modelID.value))
const providerLabel = computed(() => connectionID.value === 'managed' ? 'Google Gemini' : connection.value?.base_url ? new URL(connection.value.base_url).hostname : catalog.value?.providers.find(p => p.id === connection.value?.provider)?.name || props.initial.provider_label || '')
const selectionValid = computed(() => !loading.value && !loadError.value && (connectionID.value === 'managed' ? catalog.value?.managed.available !== false : !!connection.value?.enabled && !!selectedModel.value && catalog.value?.providers.some(p => p.enabled && p.id === connection.value?.provider)))
let loadGeneration = 0
async function loadChoices() {
  if (!can('ai_connection:read')) {
    // Existing managed rules require no customer connection permission.
    if (props.initial.billing_mode === 'customer') loadError.value = t('aiProviders.forbidden')
    return
  }
  const generation = ++loadGeneration
  loading.value = true; loadError.value = ''
  try {
    const [p, c] = await Promise.all([$apiFetch<AIProviderCatalog>('/v1/ai/providers'), $apiFetch<{connections: AIConnection[]}>('/v1/ai/connections')])
    if (generation !== loadGeneration) return
    catalog.value = p; connections.value = c.connections || []
    if (props.initial.enabled && !contractChanged.value) acknowledged.value = Boolean(props.initial.acknowledged)
    if (!props.initial.enabled) {
      const preferred = connections.value.find(c => c.is_default && c.enabled && c.models?.some(m => m.validated))
      if (preferred) { connectionID.value = preferred.id; modelID.value = preferredModel(preferred) }
    }
  } catch (e) { if (generation === loadGeneration) loadError.value = localizedError.message(e) }
  finally { if (generation === loadGeneration) loading.value = false }
}
function chooseModel() { acknowledged.value = false; categories.value = categories.value.filter(categoryAvailable) }
function chooseConnection() {
  modelID.value = preferredModel(connection.value)
  acknowledged.value = false
  categories.value = categories.value.filter(categoryAvailable)
}
onBeforeUnmount(() => { loadGeneration++ })
const privacyURL = ref('')
const categories = ref<AIInputCategory[]>([])
const acknowledged = ref(false)

const contractChanged = computed(() => {
  if (!props.initial.enabled) return true
  const stored = [...(props.initial.input_categories || [])].sort()
  const selected = [...categories.value].sort()
  return stored.length !== selected.length
    || stored.some((category, index) => category !== selected[index])
    || (props.initial.privacy_policy_url || '') !== privacyURL.value.trim()
    || (props.initial.connection_id || 'managed') !== connectionID.value
    || (props.initial.billing_mode === 'customer' && (props.initial.model !== modelID.value || props.initial.config_version !== connection.value?.config_version))
})

const valid = computed(() =>
  selectionValid.value && categories.value.every(categoryAvailable) && categories.value.length > 0 && acknowledged.value && validPrivacyURL(privacyURL.value),
)

const blockedReason = computed(() => {
  if (valid.value) return ''
  if (!selectionValid.value || !categories.value.every(categoryAvailable)) return t('aiProviders.selectModel')
  if (categories.value.length === 0) return t('projects.editor.ai.selectContent')
  if (!validPrivacyURL(privacyURL.value)) return t('projects.editor.ai.privacyURLInvalid')
  return t('projects.editor.ai.acknowledgmentRequired')
})

watch(() => props.open, (open) => {
  if (!open) { loadGeneration++; return }
  connectionID.value = props.initial.connection_id || 'managed'
  modelID.value = props.initial.model || ''
  loadError.value = ''
  void loadChoices()
  privacyURL.value = props.initial.privacy_policy_url || props.defaultPrivacyUrl || ''
  categories.value = props.initial.enabled
    ? [...(props.initial.input_categories || [])]
    : ['feedback_text']
  categories.value = categories.value.filter(category => category === 'feedback_text' || (category === 'screenshot' ? props.screenshot : category === 'audio' ? props.audio : props.video))
  if (categories.value.length === 0) categories.value = ['feedback_text']
  // A configuration that is already acknowledged stays acknowledged while it is unchanged, the
  // same rule the API applies when it decides whether to re-stamp the acknowledgement. Reopening
  // the dialog to review it therefore never silently drops an authorization already given.
  acknowledged.value = Boolean(props.initial.enabled && props.initial.acknowledged)
    && !contractChanged.value
}, { immediate: true })

// Changing what is sent to the provider is a new authorization, so a stored confirmation stops
// counting for the changed contract and has to be given again.
watch(() => JSON.stringify([privacyURL.value, categories.value, connectionID.value, modelID.value]), () => {
  if (contractChanged.value) acknowledged.value = false
})

const categoryHints = computed(() => Object.fromEntries((['feedback_text', 'screenshot', 'audio', 'video'] as AIInputCategory[]).map(category => [category, categoryAvailable(category) ? undefined : t('aiProviders.unsupported')])))
function preferredModel(c?: AIConnection) { const valid = c?.models?.filter(m => m.validated) || []; return valid.find(m => m.id === c?.default_model)?.id || valid[0]?.id || '' }
function categoryAvailable(category: AIInputCategory) {
  if (connectionID.value !== 'managed' && !selectedModel.value?.input_categories.includes(category)) return false
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
    policy_version: connectionID.value === 'managed' ? '2026-07-24' : '2026-10-02',
    provider: connectionID.value === 'managed' ? 'google_gemini' : connection.value!.provider,
    ...(connectionID.value !== 'managed' ? { billing_mode: 'customer' as const, connection_id: connection.value!.id, config_version: connection.value!.config_version, model: modelID.value } : {}),
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
        <p v-if="loading" role="status">{{ t('aiProviders.loading') }}</p>
        <p v-if="loadError" role="alert">{{ loadError }} <button class="app-button" @click="loadChoices">{{ t('aiProviders.retry') }}</button></p>
        <label>{{ t('aiProviders.selectConnection') }}
          <select v-model="connectionID" class="app-select" :disabled="loading" @change="chooseConnection">
            <option v-if="catalog?.managed.available !== false" value="managed">{{ t('aiProviders.managed') }}</option>
            <option v-for="c in connections" :key="c.id" :value="c.id" :disabled="!c.enabled || !c.models?.length">{{ c.name }}</option>
          </select>
        </label>
        <label v-if="connectionID !== 'managed'">{{ t('aiProviders.model') }}
          <select v-model="modelID" :aria-label="t('aiProviders.model')" class="app-select" @change="chooseModel"><option value="" disabled>{{ t('aiProviders.selectModel') }}</option><option v-for="m in models" :key="m.id" :value="m.id">{{ m.name || m.id }}</option></select>
          <small>{{ t('aiProviders.customer') }}</small>
        </label>
        <NuxtLink v-if="can('ai_connection:read')" to="/settings?tab=ai">{{ t('aiProviders.setup') }}</NuxtLink>
        <div class="ai-processing-note" role="note">
          <ShieldCheck :size="18" />
          <p><b>{{ t('projects.editor.ai.providerTitle', { provider: providerLabel }) }}</b><span>{{ t('projects.editor.ai.providerDescription', { provider: providerLabel }) }}</span></p>
        </div>

        <fieldset>
          <legend>{{ t('projects.editor.ai.inputTitle') }}</legend>
          <label><input data-ai-category type="checkbox" :checked="categories.includes('feedback_text')" :disabled="!categoryAvailable('feedback_text')" :title="categoryHints.feedback_text" @change="toggleCategory('feedback_text', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.feedback_text') }}</label>
          <label v-if="screenshot"><input type="checkbox" :checked="categories.includes('screenshot')" :disabled="!categoryAvailable('screenshot')" :title="categoryHints.screenshot" @change="toggleCategory('screenshot', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.screenshot') }}</label>
          <label v-if="audio"><input type="checkbox" :checked="categories.includes('audio')" :disabled="!categoryAvailable('audio')" :title="categoryHints.audio" @change="toggleCategory('audio', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.audio') }}</label>
          <label v-if="video"><input type="checkbox" :checked="categories.includes('video')" :disabled="!categoryAvailable('video')" :title="categoryHints.video" @change="toggleCategory('video', ($event.target as HTMLInputElement).checked)" /> {{ t('projects.editor.ai.inputs.video') }}</label>
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
          <span>{{ t('projects.editor.ai.acknowledgment', { provider: providerLabel }) }}</span>
        </label>
      </div>

      <footer class="app-modal__actions">
        <p v-if="blockedReason" class="ai-processing-blocked" role="status">{{ blockedReason }}</p>
        <p v-else class="ai-processing-blocked">{{ t('projects.editor.ai.saveReminder') }}</p>
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
.ai-processing-acknowledgment { padding: 12px; background: var(--surface-subtle); border: 1px solid var(--line); border-radius: 6px; font-size: 12px; line-height: 1.5; }
.ai-processing-blocked { margin: 0 auto 0 0; color: var(--muted); font-size: 12px; line-height: 1.4; text-align: left; }
@media (max-width: 560px) {
  .ai-processing-dialog { width: calc(100vw - 16px); max-height: calc(100dvh - 16px); }
  .ai-processing-body fieldset { display: grid; }
}
</style>
