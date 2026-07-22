<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  Check,
  Clipboard,
  Code2,
  Plus,
  Save,
  Settings2,
  Trash2,
} from '@lucide/vue'
import type { Project, WidgetRule, WidgetRuleInput } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDateTime } from '~/utils/format'
import { defaultWidgetRule, widgetEmbedCode } from '~/utils/widget-setup'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { can, canWrite, tenant, workspaceWritable } = useAuth()
const config = useRuntimeConfig()
const projectID = computed(() => String(route.params.id || ''))
const selectedRuleID = ref('')
const editingNew = ref(false)
const pending = ref(false)
const formError = ref('')
const notice = ref('')
const projectName = ref('')
const projectDomains = ref('')
const copied = ref(false)

const ruleForm = reactive<WidgetRuleInput>(defaultWidgetRule())

const { data, status, error, refresh } = await useAsyncData(
  () => `project:${projectID.value}`,
  async () => {
    const [project, rules] = await Promise.all([
      $apiFetch<{ project: Project }>(`/v1/projects/${projectID.value}`),
      can('widget_rule:read')
        ? $apiFetch<{ widget_rules: WidgetRule[] }>(`/v1/projects/${projectID.value}/widget-rules`)
        : Promise.resolve({ widget_rules: [] as WidgetRule[] }),
    ])
    return { project: project.project, rules: rules.widget_rules }
  },
  { watch: [projectID] },
)

watch(data, (value) => {
  if (!value) return
  projectName.value = value.project.name
  projectDomains.value = value.project.allowed_domains.join('\n')
  if (!editingNew.value) {
    const selected = value.rules.find((rule) => rule.id === selectedRuleID.value) || value.rules[0]
    if (selected) selectRule(selected)
    else startNewRule()
  }
}, { immediate: true })

const selectedRule = computed(() => data.value?.rules.find((rule) => rule.id === selectedRuleID.value))
const supportsAudio = computed(() => tenant.value?.plan !== 'starter')
const supportsDOM = computed(() => tenant.value?.plan !== 'starter')
const supportsReplay = computed(() => tenant.value?.plan !== 'starter')
const supportsVideo = computed(() => tenant.value?.plan === 'scale' || tenant.value?.plan === 'enterprise')
const embedCode = computed(() => widgetEmbedCode(
  data.value?.project.public_key || '',
  String(config.public.baseDomain || 'fidibeki.com'),
))
const connectedAt = computed(() => data.value?.project.widget_last_seen_at || '')

function selectRule(rule: WidgetRule) {
  selectedRuleID.value = rule.id
  editingNew.value = false
  Object.assign(ruleForm, {
    name: rule.name, priority: rule.priority, enabled: rule.enabled,
    matcher_type: rule.matcher_type, matcher_value: rule.matcher_value ?? null,
    trigger_type: rule.trigger_type, trigger_delay_seconds: rule.trigger_delay_seconds ?? null,
    trigger_scroll_percent: rule.trigger_scroll_percent ?? null, can_rate: rule.can_rate,
    can_write_text: rule.can_write_text, can_record_audio: rule.can_record_audio,
    can_record_video: rule.can_record_video, can_attach_element: rule.can_attach_element,
    can_attach_screenshot: rule.can_attach_screenshot, greeting: rule.greeting ?? null,
    position: rule.position, theme: rule.theme, accent_color: rule.accent_color ?? null,
    replay_mode: rule.replay_mode, replay_sampling_rate: rule.replay_sampling_rate,
    replay_max_duration_seconds: rule.replay_max_duration_seconds,
    replay_max_events: rule.replay_max_events, replay_mask_all_inputs: rule.replay_mask_all_inputs,
    replay_mask_text_selector: rule.replay_mask_text_selector ?? null,
    recurrence_mode: rule.recurrence_mode || 'cooldown',
    recurrence_days: (rule.recurrence_mode || 'cooldown') === 'cooldown' ? rule.recurrence_days ?? 30 : null,
  })
}

function startNewRule() {
  selectedRuleID.value = ''
  editingNew.value = true
  Object.assign(ruleForm, defaultWidgetRule())
}

function normalizedRule(): WidgetRuleInput {
  return {
    ...ruleForm,
    matcher_value: ruleForm.matcher_type === 'all_pages' ? null : ruleForm.matcher_value || null,
    trigger_delay_seconds: ruleForm.trigger_type === 'delay' ? Number(ruleForm.trigger_delay_seconds || 0) : null,
    trigger_scroll_percent: ruleForm.trigger_type === 'scroll' ? Number(ruleForm.trigger_scroll_percent || 1) : null,
    greeting: ruleForm.greeting?.trim() || null,
    accent_color: ruleForm.accent_color?.trim() || null,
    replay_sampling_rate: ruleForm.replay_mode === 'sampling' ? Number(ruleForm.replay_sampling_rate || 1) : 0,
    replay_mask_text_selector: ruleForm.replay_mask_text_selector?.trim() || null,
    recurrence_days: ruleForm.recurrence_mode === 'cooldown' ? Number(ruleForm.recurrence_days || 30) : null,
  }
}

async function saveRule() {
  const permission = editingNew.value ? 'widget_rule:create' : 'widget_rule:update'
  if (!canWrite(permission) || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    if (editingNew.value) {
      const response = await $apiFetch<{ widget_rule: WidgetRule }>(`/v1/projects/${projectID.value}/widget-rules`, { method: 'POST', body: normalizedRule() })
      selectedRuleID.value = response.widget_rule.id
      editingNew.value = false
    } else {
      await $apiFetch(`/v1/projects/${projectID.value}/widget-rules/${selectedRuleID.value}`, { method: 'PATCH', body: normalizedRule() })
    }
    notice.value = 'Widget rule saved.'
    await refresh()
  } catch (saveError) {
    formError.value = apiErrorMessage(saveError, 'Widget rule could not be saved.')
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function deleteRule() {
  if (!canWrite('widget_rule:delete') || !selectedRule.value || pending.value || !window.confirm(`Delete “${selectedRule.value.name}”?`)) return
  pending.value = true
  formError.value = ''
  try {
    await $apiFetch(`/v1/projects/${projectID.value}/widget-rules/${selectedRule.value.id}`, { method: 'DELETE' })
    selectedRuleID.value = ''
    editingNew.value = false
    await refresh()
  } catch (deleteError) {
    formError.value = apiErrorMessage(deleteError, 'Widget rule could not be deleted.')
  } finally { pending.value = false }
}

async function saveProject() {
  if (!canWrite('project:update') || !data.value || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    await $apiFetch(`/v1/projects/${projectID.value}`, { method: 'PATCH', body: {
      name: projectName.value,
      allowed_domains: projectDomains.value.split(/[\n,]+/).map((value) => value.trim()).filter(Boolean),
    } })
    notice.value = 'Project settings saved.'
    await refresh()
  } catch (saveError) {
    formError.value = apiErrorMessage(saveError, 'Project settings could not be saved.')
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function deleteProject() {
  if (!canWrite('project:delete') || !data.value || pending.value || !window.confirm(`Delete ${data.value.project.name} and all of its feedback?`)) return
  pending.value = true
  try {
    await $apiFetch(`/v1/projects/${projectID.value}`, { method: 'DELETE' })
    await navigateTo('/projects')
  } catch (deleteError) {
    formError.value = apiErrorMessage(deleteError, 'Project could not be deleted.')
    pending.value = false
  }
}

async function copyEmbed() {
  try {
    await navigator.clipboard.writeText(embedCode.value)
    copied.value = true
    window.setTimeout(() => { copied.value = false }, 1800)
  } catch {
    formError.value = 'The install code could not be copied. Select the code and copy it manually.'
  }
}

useHead(() => ({ title: `${data.value?.project.name || 'Project'} · Fidibeki` }))
</script>

<template>
  <div class="project-workspace">
    <header class="project-workspace-head">
      <NuxtLink to="/projects" class="app-button"><ArrowLeft :size="15" /> Projects</NuxtLink>
      <div v-if="data"><span class="project-workspace-mark">{{ data.project.name.slice(0, 1).toUpperCase() }}</span><p><b>{{ data.project.name }}</b><span>{{ data.project.allowed_domains.join(', ') || 'No domains' }}</span></p></div>
    </header>
    <section v-if="status === 'pending'" class="app-state"><div><span class="app-spinner" /><h2>Loading project</h2></div></section>
    <section v-else-if="error" class="app-state"><div><AlertTriangle :size="24" /><h2>Project unavailable</h2><p>{{ apiErrorMessage(error) }}</p></div></section>
    <template v-else-if="data">
      <section class="project-config-band">
        <div><p class="app-eyebrow">Project configuration</p><h1>{{ data.project.name }}</h1><span>{{ data.rules.length }} widget rule{{ data.rules.length === 1 ? '' : 's' }}</span><p class="project-connection" :data-connected="Boolean(connectedAt)"><i />{{ connectedAt ? `Connected ${formatDateTime(connectedAt)}` : 'Awaiting widget connection' }}</p></div>
        <div class="project-embed"><Code2 :size="15" /><code>{{ embedCode }}</code><button title="Copy embed code" @click="copyEmbed"><Check v-if="copied" :size="15" /><Clipboard v-else :size="15" /></button></div>
      </section>
      <p v-if="formError" class="project-config-error" role="alert"><AlertTriangle :size="15" /> {{ formError }}</p>
      <div class="project-config-grid">
        <aside class="project-rule-list">
          <header><span>Widget rules</span><button v-if="canWrite('widget_rule:create')" title="New widget rule" @click="startNewRule"><Plus :size="16" /></button></header>
          <button v-for="rule in data.rules" :key="rule.id" :data-active="rule.id === selectedRuleID" @click="selectRule(rule)"><span :data-enabled="rule.enabled" /><p><b>{{ rule.name }}</b><small>{{ rule.matcher_type.replaceAll('_', ' ') }} · priority {{ rule.priority }}</small></p></button>
          <p v-if="!data.rules.length" class="project-rule-empty">No rules configured.</p>
          <nav><a href="#project-settings"><Settings2 :size="14" /> Project settings</a></nav>
        </aside>

        <main class="project-rule-editor">
          <header><div><p class="app-eyebrow">{{ editingNew ? 'New rule' : 'Editing rule' }}</p><h2>{{ ruleForm.name || 'Untitled rule' }}</h2></div><div><button v-if="selectedRule && canWrite('widget_rule:delete')" class="app-button project-delete-button" :disabled="pending" title="Delete rule" aria-label="Delete rule" @click="deleteRule"><Trash2 :size="14" /></button><button class="app-button app-button--primary" type="submit" form="widget-rule-form" :disabled="pending || !(editingNew ? canWrite('widget_rule:create') : canWrite('widget_rule:update'))"><Save :size="14" /> {{ pending ? 'Saving…' : 'Save rule' }}</button></div></header>
          <form id="widget-rule-form" @submit.prevent="saveRule">
            <fieldset :disabled="!workspaceWritable">
            <section><h3>Rule identity</h3><div class="project-form-grid"><label class="wide">Name<input v-model.trim="ruleForm.name" class="app-input" required maxlength="120" /></label><label>Priority<input v-model.number="ruleForm.priority" class="app-input" type="number" /></label><label class="project-toggle"><input v-model="ruleForm.enabled" type="checkbox" /><span>Enabled</span></label></div></section>
            <section><h3>Page matching</h3><div class="project-form-grid"><label>Matcher<select v-model="ruleForm.matcher_type" class="app-select"><option value="all_pages">All pages</option><option value="url_exact">Exact URL</option><option value="url_prefix">URL prefix</option><option value="url_regex">URL regular expression</option></select></label><label v-if="ruleForm.matcher_type !== 'all_pages'" class="wide">Matcher value<input v-model="ruleForm.matcher_value" class="app-input" required /></label></div></section>
            <section><h3>Trigger</h3><div class="project-form-grid"><label>Trigger type<select v-model="ruleForm.trigger_type" class="app-select"><option value="delay">Delay</option><option value="scroll">Scroll depth</option><option value="exit_intent">Exit intent</option></select></label><label v-if="ruleForm.trigger_type === 'delay'">Delay seconds<input v-model.number="ruleForm.trigger_delay_seconds" class="app-input" type="number" min="0" max="86400" /></label><label v-if="ruleForm.trigger_type === 'scroll'">Scroll percent<input v-model.number="ruleForm.trigger_scroll_percent" class="app-input" type="number" min="1" max="100" /></label></div></section>
            <section><h3>Recurrence</h3><div class="project-form-grid"><label>Show again<select v-model="ruleForm.recurrence_mode" class="app-select"><option value="cooldown">After a period</option><option value="session">Next browser session</option><option value="never">Never show again</option></select></label><label v-if="ruleForm.recurrence_mode === 'cooldown'">Cooldown days<input v-model.number="ruleForm.recurrence_days" class="app-input" type="number" min="1" max="3650" required /></label></div></section>
            <section><h3>Feedback methods</h3><div class="project-capability-grid"><label><input v-model="ruleForm.can_rate" type="checkbox" /><span><b>Rating</b><small>Collect a 1–5 score</small></span></label><label><input v-model="ruleForm.can_write_text" type="checkbox" /><span><b>Written feedback</b><small>Allow free-form detail</small></span></label><label :data-locked="!supportsAudio"><input v-model="ruleForm.can_record_audio" type="checkbox" :disabled="!supportsAudio" /><span><b>Audio</b><small>{{ supportsAudio ? 'Capture a voice note' : 'Upgrade required' }}</small></span></label><label :data-locked="!supportsVideo"><input v-model="ruleForm.can_record_video" type="checkbox" :disabled="!supportsVideo" /><span><b>Video</b><small>{{ supportsVideo ? 'Capture camera video' : 'Scale plan required' }}</small></span></label><label :data-locked="!supportsDOM"><input v-model="ruleForm.can_attach_element" type="checkbox" :disabled="!supportsDOM" /><span><b>Page element</b><small>{{ supportsDOM ? 'Attach selected DOM context' : 'Upgrade required' }}</small></span></label><label :data-locked="!supportsDOM"><input v-model="ruleForm.can_attach_screenshot" type="checkbox" :disabled="!supportsDOM" /><span><b>Screenshot</b><small>{{ supportsDOM ? 'Capture the current view' : 'Upgrade required' }}</small></span></label></div></section>
            <section><h3>Appearance</h3><div class="project-form-grid"><label class="wide">Greeting<input v-model="ruleForm.greeting" class="app-input" maxlength="500" /></label><label>Position<select v-model="ruleForm.position" class="app-select"><option value="bottom-left">Bottom left</option><option value="bottom-right">Bottom right</option></select></label><label>Theme<select v-model="ruleForm.theme" class="app-select"><option value="light">Light</option><option value="dark">Dark</option><option value="auto">System</option></select></label><label>Accent color<input v-model="ruleForm.accent_color" class="app-input" type="color" /></label></div></section>
            <section><h3>Session replay</h3><div class="project-form-grid"><label>Replay mode<select v-model="ruleForm.replay_mode" class="app-select" :disabled="!supportsReplay"><option value="none">Disabled</option><option value="full">Every session</option><option value="sampling">Sample sessions</option></select></label><label v-if="ruleForm.replay_mode === 'sampling'">Sampling percent<input v-model.number="ruleForm.replay_sampling_rate" class="app-input" type="number" min="1" max="100" /></label><label>Max duration (seconds)<input v-model.number="ruleForm.replay_max_duration_seconds" class="app-input" type="number" min="1" max="1800" /></label><label>Max events<input v-model.number="ruleForm.replay_max_events" class="app-input" type="number" min="1" max="50000" /></label><label class="project-toggle wide"><input v-model="ruleForm.replay_mask_all_inputs" type="checkbox" /><span>Mask all input values</span></label><label class="wide">Additional text mask selector<input v-model="ruleForm.replay_mask_text_selector" class="app-input" placeholder=".private, [data-sensitive]" /></label></div></section>
            </fieldset>
          </form>
        </main>

        <aside class="project-widget-preview">
          <header><span>Live preview</span><small>{{ ruleForm.theme }} · {{ ruleForm.position }}</small></header>
          <div class="widget-preview-page" :data-theme="ruleForm.theme" inert aria-hidden="true"><div class="widget-preview-site"><span /><span /><span /></div><div class="widget-preview-copy"><b>Checkout</b><span>Review your order before payment.</span><div /></div><button class="widget-preview-trigger" :style="{ background: ruleForm.accent_color || '#087f73' }">Feedback</button><section class="widget-preview-modal"><p>{{ ruleForm.greeting || 'How was your experience?' }}</p><div v-if="ruleForm.can_rate" class="widget-preview-ratings"><button v-for="value in 5" :key="value">{{ value }}</button></div><textarea v-if="ruleForm.can_write_text" placeholder="Tell us more" /><div class="widget-preview-actions"><span v-if="ruleForm.can_attach_screenshot">Screenshot</span><span v-if="ruleForm.can_record_audio">Audio</span><button :style="{ background: ruleForm.accent_color || '#087f73' }">Send</button></div></section></div>
        </aside>
      </div>

      <section id="project-settings" class="project-settings app-surface"><header><div><h2>Project settings</h2><p>Update the collection name and allowed browser origins.</p></div><button class="app-button app-button--primary" type="submit" form="project-settings-form" :disabled="pending || !canWrite('project:update')"><Save :size="14" /> Save project</button></header><form id="project-settings-form" @submit.prevent="saveProject"><fieldset :disabled="!workspaceWritable"><label>Project name<input v-model.trim="projectName" class="app-input" required maxlength="120" /></label><label>Allowed domains<textarea v-model="projectDomains" /></label></fieldset></form><footer><div><b>Delete project</b><span>Permanently removes its rules, feedback, issues, and replay data.</span></div><button class="app-button project-danger" :disabled="pending || !canWrite('project:delete')" @click="deleteProject"><Trash2 :size="14" /> Delete project</button></footer></section>
    </template>
    <p v-if="notice" class="project-toast" role="status">{{ notice }}</p>
  </div>
</template>

<style scoped>
.project-connection { display: flex; align-items: center; gap: 6px; margin-top: 8px; color: var(--amber); font-size: 9px; font-weight: 700; }
.project-connection i { width: 7px; height: 7px; background: currentColor; border-radius: 50%; }
.project-connection[data-connected='true'] { color: var(--green); }
.project-workspace { min-height: calc(100vh - 56px); background: #f6f8f7; }
.project-workspace-head { display: flex; min-height: 64px; align-items: center; gap: 14px; padding: 9px 18px; background: #fff; border-bottom: 1px solid var(--line); }
.project-workspace-head > div { display: flex; align-items: center; gap: 9px; }
.project-workspace-mark { display: grid; width: 32px; height: 32px; place-items: center; background: var(--teal-soft); color: var(--teal); border-radius: 5px; font-size: 11px; font-weight: 800; }
.project-workspace-head p { display: flex; flex-direction: column; gap: 3px; margin: 0; }
.project-workspace-head b { font-size: 11px; }
.project-workspace-head p span { color: var(--muted); font-size: 9px; }
.project-config-band { display: flex; min-height: 92px; align-items: center; justify-content: space-between; gap: 20px; padding: 16px 20px; background: #fff; border-bottom: 1px solid var(--line); }
.project-config-band h1 { margin: 4px 0; font-size: 20px; }
.project-config-band > div:first-child > span { color: var(--muted); font-size: 9px; }
.project-embed { display: flex; width: min(520px, 52%); height: 40px; align-items: center; gap: 8px; padding-left: 10px; background: #f3f6f5; border: 1px solid var(--line); border-radius: 5px; }
.project-embed code { flex: 1; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.project-embed button { display: grid; width: 38px; height: 38px; place-items: center; background: transparent; color: var(--teal); border: 0; border-left: 1px solid var(--line); }
.project-config-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px 18px; background: var(--red-soft); color: var(--red); border-bottom: 1px solid #efc2c6; font-size: 10px; }
.project-config-grid { display: grid; min-height: 720px; grid-template-columns: 210px minmax(480px, 1fr) minmax(300px, 34%); border-bottom: 1px solid var(--line); }
.project-rule-list { background: #fafbfa; border-right: 1px solid var(--line); }
.project-rule-list > header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; padding: 0 10px 0 14px; color: var(--muted); border-bottom: 1px solid var(--line); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.project-rule-list > header button { display: grid; width: 30px; height: 30px; place-items: center; background: transparent; border: 0; border-radius: 4px; }
.project-rule-list > button { display: grid; width: 100%; min-height: 56px; grid-template-columns: 8px minmax(0, 1fr); align-items: center; gap: 9px; padding: 7px 12px; background: transparent; color: var(--ink-soft); border: 0; border-bottom: 1px solid var(--line); text-align: left; }
.project-rule-list > button:hover,
.project-rule-list > button[data-active='true'] { background: #eaf3f0; }
.project-rule-list > button > span { width: 7px; height: 7px; background: var(--line-strong); border-radius: 50%; }
.project-rule-list > button > span[data-enabled='true'] { background: var(--green); }
.project-rule-list button p { display: flex; min-width: 0; flex-direction: column; gap: 3px; margin: 0; }
.project-rule-list button b,
.project-rule-list button small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.project-rule-list button b { font-size: 10px; }
.project-rule-list button small { color: var(--muted); font-size: 8px; }
.project-rule-empty { padding: 20px 12px; color: var(--muted); font-size: 9px; text-align: center; }
.project-rule-list nav { padding: 10px; border-top: 1px solid var(--line); }
.project-rule-list nav a { display: flex; min-height: 34px; align-items: center; gap: 7px; padding: 0 8px; color: var(--muted); border-radius: 4px; font-size: 9px; text-decoration: none; }
.project-rule-editor { min-width: 0; background: #fff; border-right: 1px solid var(--line); }
.project-rule-editor > header { position: sticky; top: 56px; z-index: 20; display: flex; min-height: 65px; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px; background: #fff; border-bottom: 1px solid var(--line); }
.project-rule-editor > header h2 { margin: 3px 0 0; font-size: 14px; }
.project-rule-editor > header > div:last-child { display: flex; gap: 6px; }
.project-delete-button { width: 38px; padding: 0; color: var(--red); }
.project-rule-editor form > fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
.project-rule-editor form > fieldset > section { padding: 17px 18px; border-bottom: 1px solid var(--line); }
.project-rule-editor form h3 { margin: 0 0 13px; font-size: 11px; }
.project-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.project-form-grid label { display: flex; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.project-form-grid label.wide { grid-column: 1 / -1; }
.project-toggle { align-items: center; justify-content: flex-start; flex-direction: row !important; gap: 8px !important; min-height: 38px; padding-top: 17px; color: var(--ink-soft) !important; }
.project-capability-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.project-capability-grid label { display: flex; min-height: 58px; align-items: center; gap: 9px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 5px; }
.project-capability-grid label[data-locked='true'] { background: #f7f8f8; opacity: .65; }
.project-capability-grid label span { display: flex; flex-direction: column; gap: 3px; }
.project-capability-grid b { font-size: 10px; }
.project-capability-grid small { color: var(--muted); font-size: 8px; }
.project-widget-preview { position: sticky; top: 56px; height: calc(100vh - 56px); min-height: 620px; background: #edf2f0; }
.project-widget-preview > header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; padding: 0 14px; background: #f8faf9; color: var(--muted); border-bottom: 1px solid var(--line); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.project-widget-preview header small { font-size: 8px; font-weight: 500; }
.widget-preview-page { position: relative; height: calc(100% - 48px); min-height: 570px; overflow: hidden; padding: 24px; background: #fff; }
.widget-preview-page[data-theme='dark'] { background: #202825; color: #fff; }
.widget-preview-site { display: flex; gap: 5px; padding-bottom: 14px; border-bottom: 1px solid var(--line); }
.widget-preview-site span { width: 7px; height: 7px; background: var(--line-strong); border-radius: 50%; }
.widget-preview-copy { display: flex; flex-direction: column; gap: 8px; margin-top: 28px; }
.widget-preview-copy b { font-size: 16px; }
.widget-preview-copy > span { color: var(--muted); font-size: 9px; }
.widget-preview-copy div { height: 170px; margin-top: 10px; background: #edf2f0; border-radius: 5px; }
.widget-preview-trigger { position: absolute; right: 18px; bottom: 18px; min-height: 38px; padding: 0 13px; color: #fff; border: 0; border-radius: 5px; font-size: 10px; font-weight: 750; }
.widget-preview-modal { position: absolute; right: 18px; bottom: 66px; left: 18px; padding: 14px; background: #fff; color: var(--ink); border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.widget-preview-modal p { margin: 0 0 10px; font-size: 11px; font-weight: 700; }
.widget-preview-ratings { display: grid; grid-template-columns: repeat(5, 1fr); gap: 4px; margin-bottom: 8px; }
.widget-preview-ratings button { height: 30px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.widget-preview-modal textarea { width: 100%; height: 62px; padding: 7px; border: 1px solid var(--line); border-radius: 4px; resize: none; font: inherit; font-size: 9px; }
.widget-preview-actions { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
.widget-preview-actions span { color: var(--muted); font-size: 8px; }
.widget-preview-actions button { min-height: 30px; margin-left: auto; padding: 0 11px; color: #fff; border: 0; border-radius: 4px; font-size: 9px; font-weight: 700; }
.project-settings { margin: 22px; overflow: hidden; }
.project-settings > header { display: flex; min-height: 66px; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.project-settings h2 { margin: 0; font-size: 13px; }
.project-settings header p { margin: 4px 0 0; color: var(--muted); font-size: 9px; }
.project-settings > form { padding: 16px; }
.project-settings > form > fieldset { display: grid; min-width: 0; grid-template-columns: 1fr 1.4fr; gap: 14px; margin: 0; padding: 0; border: 0; }
.project-settings label { display: flex; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.project-settings textarea { min-height: 96px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 10px; }
.project-settings > footer { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 14px 16px; background: #fff8f8; border-top: 1px solid #efc2c6; }
.project-settings footer > div { display: flex; flex-direction: column; gap: 3px; }
.project-settings footer b { color: var(--red); font-size: 10px; }
.project-settings footer span { color: var(--muted); font-size: 9px; }
.project-danger { color: var(--red); border-color: #e4adb4; }
.project-toast { position: fixed; right: 20px; bottom: 20px; z-index: 90; margin: 0; padding: 10px 13px; background: #17342f; color: #fff; border-radius: 5px; box-shadow: var(--shadow-overlay); font-size: 10px; }
@media (max-width: 1180px) { .project-config-grid { grid-template-columns: 190px minmax(480px, 1fr); } .project-widget-preview { display: none; } }
@media (max-width: 800px) { .project-config-band { align-items: stretch; flex-direction: column; } .project-embed { width: 100%; } .project-config-grid { display: block; } .project-rule-list { display: flex; overflow-x: auto; border: 0; border-bottom: 1px solid var(--line); } .project-rule-list > header { min-width: 130px; } .project-rule-list > button { min-width: 180px; border-right: 1px solid var(--line); } .project-rule-list nav { display: none; } .project-rule-editor { border: 0; } .project-settings > form > fieldset { grid-template-columns: 1fr; } }
@media (max-width: 600px) { .project-form-grid, .project-capability-grid { grid-template-columns: 1fr; } .project-form-grid label.wide { grid-column: auto; } .project-rule-editor > header { align-items: flex-start; flex-direction: column; } .project-settings { margin: 12px; } .project-settings > footer { align-items: stretch; flex-direction: column; } }
</style>
