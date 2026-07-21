<script setup lang="ts">
import {
  AlertTriangle,
  ArrowRight,
  Check,
  CheckCircle2,
  Clipboard,
  Code2,
  FolderPlus,
  RefreshCw,
  Radio,
} from '@lucide/vue'
import type { Project, WidgetRule } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { defaultWidgetRule, widgetEmbedCode } from '~/utils/widget-setup'

const { $apiFetch } = useNuxtApp()
const { tenant } = useAuth()
const config = useRuntimeConfig()
const stage = ref<'project' | 'rule' | 'install'>('project')
const project = ref<Project | null>(null)
const projectName = ref(tenant.value?.name ? `${tenant.value.name} website` : 'Main website')
const domains = ref('')
const pending = ref(false)
const errorMessage = ref('')
const copied = ref(false)
const connected = computed(() => Boolean(project.value?.widget_last_seen_at))
const embedCode = computed(() => project.value
  ? widgetEmbedCode(project.value.public_key, String(config.public.baseDomain || 'fidibeki.com'))
  : '')

const { data: existing, error: loadError, refresh: refreshExisting } = await useAsyncData(
  'activation-projects',
  () => $apiFetch<{ projects: Project[] }>('/v1/projects'),
)

if (existing.value?.projects.length) {
  await navigateTo(`/projects/${existing.value.projects[0].id}`, { replace: true })
}

watch(existing, async (value) => {
  if (value?.projects.length) await navigateTo(`/projects/${value.projects[0].id}`, { replace: true })
})

let pollTimer: ReturnType<typeof setTimeout> | undefined
let foregroundPolls = 0

function parsedDomains() {
  return domains.value.split(/[\n,]+/).map(value => value.trim()).filter(Boolean)
}

function stopPolling() {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = undefined
}

function schedulePoll() {
  stopPolling()
  if (!connected.value && foregroundPolls < 24) pollTimer = setTimeout(pollConnection, 5000)
}

async function pollConnection() {
  if (!project.value || connected.value) return
  if (document.hidden) {
    schedulePoll()
    return
  }
  foregroundPolls += 1
  try {
    const response = await $apiFetch<{ project: Project }>(`/v1/projects/${project.value.id}`)
    project.value = response.project
  } catch {
    // A later poll can recover without hiding the installation code.
  }
  schedulePoll()
}

function startPolling() {
  foregroundPolls = 0
  schedulePoll()
}

async function createRule() {
  if (!project.value || pending.value) return
  pending.value = true
  stage.value = 'rule'
  errorMessage.value = ''
  try {
    await $apiFetch<{ widget_rule: WidgetRule }>(`/v1/projects/${project.value.id}/widget-rules`, {
      method: 'POST',
      body: defaultWidgetRule(),
    })
    stage.value = 'install'
    startPolling()
  } catch (error) {
    errorMessage.value = apiErrorMessage(error, 'The default widget rule could not be created.')
  } finally {
    pending.value = false
  }
}

async function createProject() {
  if (pending.value || !parsedDomains().length) return
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await $apiFetch<{ project: Project }>('/v1/projects', {
      method: 'POST',
      body: { name: projectName.value, allowed_domains: parsedDomains() },
    })
    project.value = response.project
  } catch (error) {
    errorMessage.value = apiErrorMessage(error, 'The project could not be created.')
    pending.value = false
    return
  }
  pending.value = false
  await createRule()
}

async function copyCode() {
  try {
    await navigator.clipboard.writeText(embedCode.value)
    copied.value = true
    window.setTimeout(() => { copied.value = false }, 1800)
  } catch {
    errorMessage.value = 'The install code could not be copied. Select it and copy it manually.'
  }
}

onBeforeUnmount(stopPolling)
useHead({ title: 'Connect your first project · Fidibeki' })
</script>

<template>
  <div class="activation-page app-page">
    <header class="activation-head">
      <div><p class="app-eyebrow">Workspace activation</p><h1>Connect your first project</h1><span>{{ tenant?.name }}</span></div>
      <ol aria-label="Setup progress">
        <li :data-active="stage === 'project'" :data-complete="stage !== 'project'"><span>1</span> Project</li>
        <li :data-active="stage === 'rule'" :data-complete="stage === 'install'"><span>2</span> Rule</li>
        <li :data-active="stage === 'install'"><span>3</span> Install</li>
      </ol>
    </header>

    <section v-if="loadError" class="activation-surface activation-load-error">
      <AlertTriangle :size="22" />
      <h2>Project state unavailable</h2>
      <p>{{ apiErrorMessage(loadError) }}</p>
      <button class="app-button" @click="refreshExisting()"><RefreshCw :size="15" /> Try again</button>
    </section>

    <section v-else-if="stage === 'project'" class="activation-surface">
      <header><FolderPlus :size="20" /><div><h2>Create a project</h2><p>The allowed domains determine where the public widget key can run.</p></div></header>
      <form @submit.prevent="createProject">
        <label>Project name<input v-model.trim="projectName" class="app-input" required maxlength="120" /></label>
        <label>Allowed domains<textarea v-model="domains" required placeholder="example.com&#10;*.preview.example.com" /><small>One exact hostname or leading wildcard per line.</small></label>
        <p v-if="errorMessage" class="activation-error" role="alert"><AlertTriangle :size="15" /> {{ errorMessage }}</p>
        <button class="app-button app-button--primary" type="submit" :disabled="pending || !projectName || !parsedDomains().length">
          {{ pending ? 'Creating project…' : 'Create and configure' }} <ArrowRight v-if="!pending" :size="15" />
        </button>
      </form>
    </section>

    <section v-else-if="stage === 'rule'" class="activation-surface activation-rule-state">
      <template v-if="pending">
        <span class="app-spinner" />
        <h2>Configuring the default rule</h2>
        <p>Rating and written feedback will be available on every allowed page.</p>
      </template>
      <template v-else>
        <AlertTriangle :size="22" />
        <h2>Project created, rule not configured</h2>
        <p class="activation-error" role="alert">{{ errorMessage }}</p>
        <div><button class="app-button app-button--primary" @click="createRule"><RefreshCw :size="15" /> Retry rule</button><NuxtLink class="app-button" :to="`/projects/${project?.id}`">Open project</NuxtLink></div>
      </template>
    </section>

    <section v-else class="activation-install">
      <div class="activation-surface activation-code">
        <header><Code2 :size="20" /><div><p class="app-eyebrow">Install loader</p><h2>{{ project?.name }}</h2></div></header>
        <div class="activation-snippet"><code>{{ embedCode }}</code><button type="button" title="Copy install code" @click="copyCode"><Check v-if="copied" :size="16" /><Clipboard v-else :size="16" /></button></div>
        <p v-if="errorMessage" class="activation-error" role="alert"><AlertTriangle :size="15" /> {{ errorMessage }}</p>
        <footer><NuxtLink class="app-button" :to="`/projects/${project?.id}`">Open project configuration</NuxtLink></footer>
      </div>

      <aside class="activation-connection" :data-connected="connected">
        <CheckCircle2 v-if="connected" :size="23" />
        <Radio v-else :size="23" />
        <p class="app-eyebrow">Connection</p>
        <h2>{{ connected ? 'Widget connected' : 'Awaiting connection' }}</h2>
        <p v-if="connected && project?.widget_last_seen_at">Last seen {{ new Date(project.widget_last_seen_at).toLocaleString() }}</p>
        <p v-else>The status updates after the loader requests an authorized widget configuration.</p>
        <NuxtLink v-if="connected" class="app-button app-button--primary" to="/">Open workspace <ArrowRight :size="15" /></NuxtLink>
      </aside>
    </section>
  </div>
</template>

<style scoped>
.activation-page { width: min(1040px, 100%); margin: 0 auto; }
.activation-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 30px; margin-bottom: 22px; }
.activation-head h1 { margin: 4px 0; font-size: 23px; }
.activation-head > div > span { color: var(--muted); font-size: 11px; }
.activation-head ol { display: flex; min-width: 360px; justify-content: flex-end; gap: 0; margin: 0; padding: 0; list-style: none; }
.activation-head li { position: relative; display: flex; min-width: 105px; align-items: center; gap: 7px; color: var(--muted); font-size: 10px; font-weight: 700; }
.activation-head li:not(:last-child)::after { width: 34px; height: 1px; margin: 0 8px; background: var(--line-strong); content: ''; }
.activation-head li > span { display: grid; width: 25px; height: 25px; flex: 0 0 25px; place-items: center; background: #fff; border: 1px solid var(--line-strong); border-radius: 50%; }
.activation-head li[data-active='true'] { color: var(--teal); }
.activation-head li[data-active='true'] > span { background: var(--teal); color: #fff; border-color: var(--teal); }
.activation-head li[data-complete='true'] > span { background: var(--green-soft); color: var(--green); border-color: #acd5bf; }
.activation-surface { background: #fff; border: 1px solid var(--line); border-radius: 6px; }
.activation-surface > header { display: flex; min-height: 70px; align-items: center; gap: 11px; padding: 14px 18px; border-bottom: 1px solid var(--line); }
.activation-surface h2 { font-size: 15px; }
.activation-surface header p { margin-top: 4px; color: var(--muted); font-size: 10px; }
.activation-surface form { display: grid; grid-template-columns: .8fr 1.2fr; gap: 16px; padding: 20px; }
.activation-surface form label { display: flex; flex-direction: column; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.activation-surface textarea { min-height: 120px; padding: 9px 10px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 11px; }
.activation-surface label small { color: var(--muted); font-size: 9px; font-weight: 500; }
.activation-surface form > button, .activation-surface form > .activation-error { grid-column: 1 / -1; }
.activation-surface form > button { justify-self: end; min-width: 190px; }
.activation-error { display: flex; align-items: center; gap: 7px; color: var(--red) !important; font-size: 10px !important; }
.activation-rule-state, .activation-load-error { display: flex; min-height: 330px; align-items: center; justify-content: center; flex-direction: column; gap: 10px; padding: 34px; text-align: center; }
.activation-rule-state > p, .activation-load-error > p { max-width: 470px; color: var(--muted); font-size: 11px; }
.activation-rule-state > div { display: flex; gap: 8px; margin-top: 8px; }
.activation-install { display: grid; grid-template-columns: minmax(0, 1.5fr) minmax(260px, .65fr); gap: 14px; align-items: start; }
.activation-code > header .app-eyebrow { margin-bottom: 3px; }
.activation-snippet { display: flex; min-height: 104px; align-items: center; gap: 12px; margin: 20px; padding: 14px; background: #17231f; color: #dff3ef; border: 1px solid #0d1815; border-radius: 5px; }
.activation-snippet code { min-width: 0; flex: 1; overflow-wrap: anywhere; font-size: 10px; line-height: 1.7; }
.activation-snippet button { display: grid; width: 36px; height: 36px; flex: 0 0 36px; place-items: center; background: #2a3a35; color: #fff; border: 1px solid #4d615a; border-radius: 4px; }
.activation-code > .activation-error { margin: 0 20px; }
.activation-code footer { display: flex; justify-content: flex-end; padding: 0 20px 20px; }
.activation-connection { display: flex; min-height: 260px; align-items: flex-start; flex-direction: column; padding: 20px; background: #fff; border: 1px solid #e6cf9c; border-radius: 6px; }
.activation-connection > svg { margin-bottom: 22px; color: var(--amber); }
.activation-connection h2 { margin: 4px 0 8px; font-size: 15px; }
.activation-connection > p:not(.app-eyebrow) { color: var(--muted); font-size: 10px; line-height: 1.6; }
.activation-connection .app-button { width: 100%; margin-top: auto; }
.activation-connection[data-connected='true'] { background: #fbfefc; border-color: #acd5bf; }
.activation-connection[data-connected='true'] > svg { color: var(--green); }
@media (max-width: 800px) { .activation-head { align-items: flex-start; flex-direction: column; } .activation-head ol { width: 100%; min-width: 0; justify-content: flex-start; } .activation-head li { min-width: 0; flex: 1; } .activation-head li:not(:last-child)::after { width: 18px; } .activation-install { grid-template-columns: 1fr; } .activation-connection { min-height: 220px; } }
@media (max-width: 560px) { .activation-surface form { grid-template-columns: 1fr; padding: 15px; } .activation-surface form > button { width: 100%; justify-self: stretch; } .activation-head li { font-size: 0; } .activation-head li > span { font-size: 10px; } .activation-head li:not(:last-child)::after { flex: 1; } .activation-snippet { margin: 14px; } .activation-code footer { padding: 0 14px 14px; } .activation-code footer .app-button { width: 100%; } }
</style>
