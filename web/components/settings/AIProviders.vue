<script setup lang="ts">
import type { AIConnection, AIInputCategory, AIModel, AIProviderCatalog, AIProviderID } from '~/types/api'
const { $apiFetch } = useNuxtApp()
const { can, canWrite, tenant } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const connections = ref<AIConnection[]>([])
const catalog = ref<AIProviderCatalog | null>(null)
const loading = ref(false)
const pending = ref(false)
const error = ref('')
const notice = ref('')
const editor = ref(false)
const editorElement = ref<HTMLFormElement | null>(null)
const testElement = ref<HTMLFormElement | null>(null)
let returnFocus: HTMLElement | null = null
function focusForm(target: typeof editorElement) { void nextTick(() => target.value?.querySelector<HTMLElement>('input, select, button')?.focus()) }
function restoreFocus() { void nextTick(() => { if (returnFocus?.isConnected) returnFocus.focus() }) }
const current = ref<AIConnection | null>(null)
const removing = ref('')
const testing = ref('')
const model = ref('')
const inputs = ref<AIInputCategory[]>(['feedback_text'])
const discovered = ref<AIModel[]>([])
const blank = () => ({ name: '', provider: 'google_gemini' as AIProviderID, base_url: '', credential: '', region: 'us-east-1', auth_mode: 'api_key', role_arn: '', credential_expires_at: '', rpm: 15, concurrency: 2, enabled: true, is_default: false, default_model: '', revision: 0 })
const form = reactive(blank())
const providers = computed(() => catalog.value?.providers.filter(p => p.enabled) || [])
watch(() => form.provider, provider => { if (!current.value) form.auth_mode = provider === 'amazon_bedrock' ? 'role' : 'api_key' })
const selectedProvider = computed(() => catalog.value?.providers.find(p => p.id === form.provider))
const testConnection = computed(() => connections.value.find(c => c.id === testing.value))
const testInputs = computed(() => catalog.value?.providers.find(p => p.id === testConnection.value?.provider)?.input_categories || [])
const manageable = computed(() => canWrite('ai_connection:manage'))
let generation = 0
async function load() {
  const request = ++generation
  if (!can('ai_connection:read')) return
  loading.value = true; error.value = ''
  try {
    const [p, c] = await Promise.all([$apiFetch<AIProviderCatalog>('/v1/ai/providers'), $apiFetch<{connections: AIConnection[]}>('/v1/ai/connections')])
    if (request !== generation) return
    catalog.value = p; connections.value = c.connections || []
  } catch (e) { if (request === generation) error.value = localizedError.message(e) }
  finally { if (request === generation) loading.value = false }
}
watch(() => tenant.value?.id, () => { editor.value = false; testing.value = ''; form.credential = ''; connections.value = []; catalog.value = null; void load() }, { immediate: true })
onBeforeUnmount(() => { generation++; form.credential = '' })
function open(c?: AIConnection) {
  returnFocus = document.activeElement as HTMLElement | null
  error.value = ''; notice.value = ''; current.value = c || null
  Object.assign(form, blank(), c ? { ...c, base_url: c.base_url || '', role_arn: c.role_arn || '', default_model: c.default_model || '', credential_expires_at: c.credential_expires_at?.slice(0, 16) || '' } : { provider: providers.value[0]?.id || 'google_gemini' })
  form.credential = ''; editor.value = true; focusForm(editorElement)
}
function cancel(restore = true) { form.credential = ''; editor.value = false; if (restore) restoreFocus() }
function closeTest() { testing.value = ''; restoreFocus() }
async function save() {
  pending.value = true; error.value = ''; notice.value = ''
  // Explicit fields keep response metadata and secret-bearing bodies separate.
  const body = { name: form.name, provider: form.provider, base_url: form.base_url, credential: form.credential, region: form.region, auth_mode: form.auth_mode, role_arn: form.role_arn, credential_expires_at: form.credential_expires_at ? new Date(form.credential_expires_at + 'Z').toISOString() : null, rpm: form.rpm, concurrency: form.concurrency, enabled: form.enabled, is_default: form.is_default, default_model: form.default_model, revision: form.revision }
  try {
    const response = await $apiFetch<{connection: AIConnection}>(current.value ? `/v1/ai/connections/${current.value.id}` : '/v1/ai/connections', { method: current.value ? 'PATCH' : 'POST', body })
    cancel(false); await load(); beginTest(response.connection, false); notice.value = t('aiProviders.saved')
  } catch (e) { error.value = localizedError.message(e) }
  finally { form.credential = ''; pending.value = false }
}
function beginTest(c: AIConnection, captureFocus = true) { if (captureFocus) returnFocus = document.activeElement as HTMLElement | null; testing.value = c.id; model.value = c.default_model || c.models?.[0]?.id || ''; inputs.value = ['feedback_text']; discovered.value = c.models || []; notice.value = ''; focusForm(testElement) }
async function discover() {
  pending.value = true; error.value = ''
  try {
    const r = await $apiFetch<{models: AIModel[]; discovery_available: boolean}>(`/v1/ai/connections/${testing.value}/models`)
    discovered.value = r.models || []; if (!r.discovery_available) notice.value = t('aiProviders.discoveryFailed')
  } catch (e) { error.value = localizedError.message(e) }
  finally { pending.value = false }
}
async function validate() {
  pending.value = true; error.value = ''; notice.value = ''
  try {
    const r = await $apiFetch<{valid: boolean; failure_code?: string}>(`/v1/ai/connections/${testing.value}/validate`, { method: 'POST', body: { model: model.value.trim(), input_categories: inputs.value } })
    await load()
    if (r.valid) notice.value = t('aiProviders.valid')
    else error.value = t('aiProviders.failed', { code: r.failure_code })
  } catch (e) { error.value = localizedError.message(e) }
  finally { pending.value = false }
}
async function remove(id: string) {
  pending.value = true; error.value = ''
  try {
    const r = await $apiFetch<{status: string}>(`/v1/ai/connections/${id}`, { method: 'DELETE' })
    removing.value = ''; testing.value = ''; await load(); notice.value = t(r.status === 'cleanup_pending' ? 'aiProviders.cleanupPending' : 'aiProviders.deleted')
  } catch (e) { error.value = localizedError.message(e) }
  finally { pending.value = false }
}
</script>

<template>
  <section class="app-surface ai-settings" :aria-busy="loading || pending">
    <header><div><h2>{{ t('aiProviders.title') }}</h2><p>{{ t('aiProviders.description') }}</p></div><button v-if="manageable && providers.length" class="app-button app-button--primary" :disabled="pending" @click="open()">{{ t('aiProviders.add') }}</button></header>
    <p v-if="!can('ai_connection:read')">{{ t('aiProviders.forbidden') }}</p>
    <template v-else>
      <p v-if="loading" role="status">{{ t('aiProviders.loading') }}</p>
      <p v-if="error" class="ai-error" role="alert">{{ error }} <button class="app-button" :disabled="loading || pending" @click="load">{{ t('aiProviders.retry') }}</button></p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <p v-if="!manageable">{{ t('aiProviders.readOnly') }}</p>
      <p v-if="catalog?.managed.available">{{ t('aiProviders.managed') }} · {{ catalog.managed.model }}</p>
      <p v-if="catalog && !providers.length">{{ t('aiProviders.unavailable') }}</p>
      <p v-if="!loading && !connections.length">{{ t('aiProviders.empty') }}</p>
      <p>{{ t('aiProviders.customer') }}</p>
      <ul class="ai-connections">
        <li v-for="c in connections" :key="c.id">
          <div><b>{{ c.name }}</b><p>{{ catalog?.providers.find(p => p.id === c.provider)?.name }} <span v-if="c.base_url">· {{ c.base_url }}</span></p><small>{{ !c.enabled ? t('aiProviders.stopped') : c.models?.length ? t('aiProviders.ready') : t('aiProviders.unvalidated') }} <span v-if="c.is_default">· {{ t('aiProviders.default') }}</span></small></div>
          <div v-if="manageable" class="ai-actions"><button class="app-button" :disabled="pending" @click="open(c)">{{ t('aiProviders.edit') }}</button><button class="app-button" :disabled="pending || !c.enabled" @click="beginTest(c)">{{ t('aiProviders.validate') }}</button><button class="app-button" :disabled="pending" @click="removing = c.id">{{ t('aiProviders.remove') }}</button></div>
          <div v-if="removing === c.id" class="ai-wide"><p>{{ t('aiProviders.deleteConfirm') }}</p><button class="app-button" :disabled="pending" @click="removing = ''">{{ t('aiProviders.cancel') }}</button> <button class="app-button" :disabled="pending" @click="remove(c.id)">{{ t('aiProviders.confirmDelete') }}</button></div>
        </li>
      </ul>
      <form v-if="editor" ref="editorElement" class="ai-form" @submit.prevent="save">
        <h3 class="ai-wide">{{ current ? t('aiProviders.edit') : t('aiProviders.add') }}</h3>
        <label>{{ t('aiProviders.name') }}<input v-model.trim="form.name" class="app-input" maxlength="100" required /></label>
        <label>{{ t('aiProviders.provider') }}<select v-model="form.provider" class="app-select" :disabled="!!current"><option v-for="p in catalog?.providers" :key="p.id" :value="p.id" :disabled="!p.enabled">{{ p.name }}</option></select></label>
        <label v-if="selectedProvider?.custom_endpoint" class="ai-wide">{{ t('aiProviders.endpoint') }}<input v-model.trim="form.base_url" class="app-input" type="url" :placeholder="t('aiProviders.endpointPlaceholder')" required /></label>
        <template v-if="form.provider === 'amazon_bedrock'">
          <label>{{ t('aiProviders.region') }}<input v-model.trim="form.region" class="app-input" required /></label>
          <label>{{ t('aiProviders.auth') }}<select v-model="form.auth_mode" class="app-select"><option value="role">{{ t('aiProviders.role') }}</option><option value="api_key">{{ t('aiProviders.pilot') }}</option></select></label>
          <label v-if="form.auth_mode === 'role'" class="ai-wide">{{ t('aiProviders.roleARN') }}<input v-model.trim="form.role_arn" class="app-input" required /></label>
          <label v-else>{{ t('aiProviders.expires') }}<input v-model="form.credential_expires_at" class="app-input" type="datetime-local" required /></label>
        </template>
        <label v-if="form.provider !== 'amazon_bedrock' || form.auth_mode !== 'role'" class="ai-wide">{{ t('aiProviders.credential') }}<input v-model="form.credential" class="app-input" type="password" autocomplete="new-password" :required="!current" maxlength="16384" /><small>{{ t(current ? 'aiProviders.keepCredential' : 'aiProviders.secretNote') }}</small></label>
        <label>{{ t('aiProviders.rpm') }}<input v-model.number="form.rpm" class="app-input" type="number" min="1" max="120" required /></label>
        <label>{{ t('aiProviders.concurrency') }}<input v-model.number="form.concurrency" class="app-input" type="number" min="1" max="8" required /></label>
        <label class="ai-wide">{{ t('aiProviders.defaultModel') }}<input v-model.trim="form.default_model" class="app-input" maxlength="512" /></label>
        <label class="ai-check"><input v-model="form.enabled" type="checkbox" />{{ t('aiProviders.enabled') }}</label><label class="ai-check"><input v-model="form.is_default" type="checkbox" />{{ t('aiProviders.default') }}</label>
        <footer class="ai-wide ai-actions"><button class="app-button" type="button" :disabled="pending" @click="cancel()">{{ t('aiProviders.cancel') }}</button><button class="app-button app-button--primary" :disabled="pending">{{ t(pending ? 'aiProviders.saving' : 'aiProviders.save') }}</button></footer>
      </form>
      <form v-if="testConnection && !editor" ref="testElement" class="ai-form" @submit.prevent="validate">
        <h3 class="ai-wide">{{ t('aiProviders.validate') }} · {{ testConnection.name }}</h3>
        <div v-if="testConnection.auth_mode === 'role'" class="ai-wide"><p>{{ t('aiProviders.trust') }}</p><p>{{ t('aiProviders.callerARN') }}: <code>{{ catalog?.aws_caller_arn }}</code></p><p>{{ t('aiProviders.externalID') }}: <code>{{ testConnection.external_id }}</code></p></div>
        <label class="ai-wide">{{ t('aiProviders.model') }}<input v-model.trim="model" class="app-input" list="ai-models" maxlength="512" required /><datalist id="ai-models"><option v-for="m in discovered" :key="m.id" :value="m.id">{{ m.name }}</option></datalist><small>{{ t('aiProviders.modelHint') }}</small></label>
        <fieldset class="ai-wide"><legend>{{ t('projects.editor.ai.inputTitle') }}</legend><label v-for="category in testInputs" :key="category" class="ai-check"><input v-model="inputs" type="checkbox" :value="category" />{{ t(`projects.editor.ai.inputs.${category}`) }}</label></fieldset>
        <footer class="ai-wide ai-actions"><button class="app-button" type="button" :disabled="pending" @click="closeTest">{{ t('aiProviders.cancel') }}</button><button class="app-button" type="button" :disabled="pending" @click="discover">{{ t('aiProviders.discover') }}</button><button class="app-button app-button--primary" :disabled="pending || !model.trim() || !inputs.length || !testConnection.enabled">{{ t(pending ? 'aiProviders.validating' : 'aiProviders.validate') }}</button></footer>
      </form>
    </template>
  </section>
</template>

<style scoped>
.ai-settings { padding: 24px; }
.ai-settings header, .ai-connections li { display: flex; justify-content: space-between; align-items: start; gap: 16px; flex-wrap: wrap; }
.ai-settings h2, .ai-settings h3 { margin: 0; }
.ai-settings p, .ai-settings small { color: var(--muted); line-height: 1.5; }
.ai-settings code { overflow-wrap: anywhere; }
.ai-connections { padding: 0; list-style: none; }
.ai-connections li { padding: 18px 0; border-top: 1px solid var(--line); }
.ai-connections p { margin: 5px 0; overflow-wrap: anywhere; }
.ai-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.ai-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; padding-top: 24px; margin-top: 18px; border-top: 1px solid var(--line); }
.ai-form label { display: grid; gap: 7px; }
.ai-form .ai-check { display: flex; align-items: start; gap: 8px; }
.ai-wide { grid-column: 1 / -1; width: 100%; }
.ai-form fieldset { border: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 12px; }
.ai-form legend { margin-bottom: 12px; }
.ai-error { color: var(--red, #b42318) !important; }
@media (max-width: 650px) { .ai-settings { padding: 16px; } .ai-form { grid-template-columns: 1fr; } }
</style>
