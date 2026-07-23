<script setup lang="ts">
import { AlertTriangle, ArrowUpRight, ChevronRight, FolderKanban, Plus, X } from '@lucide/vue'
import type { Project, TenantConfiguration } from '~/types/api'

const { $apiFetch } = useNuxtApp()
const { canWrite } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { date: formatDate, integer } = useRegionalFormat()
const createOpen = ref(false)
const name = ref('')
const domains = ref('')
const pending = ref(false)
const formError = ref('')

const { data, status, error, refresh } = await useAsyncData(
  'projects-list',
  async () => {
    const [projects, configuration] = await Promise.all([
      $apiFetch<{ projects: Project[] }>('/v1/projects'),
      $apiFetch<TenantConfiguration>('/v1/config'),
    ])
    return { projects: projects.projects, configuration }
  },
)

const projectLimit = computed(() => data.value?.configuration.plan.limits.projects ?? -1)
const projectLimitReached = computed(() => projectLimit.value >= 0 && (data.value?.projects.length || 0) >= projectLimit.value)
const projectUsage = computed(() => t('projects.list.usage', {
  used: integer(data.value?.projects.length || 0),
  limit: integer(projectLimit.value),
}))

function parsedDomains() {
  return domains.value.split(/[\n,]+/).map((domain) => domain.trim()).filter(Boolean)
}

async function createProject() {
  if (!canWrite('project:create') || projectLimitReached.value || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    const response = await $apiFetch<{ project: Project }>('/v1/projects', {
      method: 'POST', body: { name: name.value, allowed_domains: parsedDomains() },
    })
    createOpen.value = false
    name.value = ''
    domains.value = ''
    await refresh()
    await navigateTo(`/projects/${response.project.id}`)
  } catch (createError) {
    formError.value = apiErrorMessage(createError, 'projects.list.errors.create')
  } finally { pending.value = false }
}

useHead(() => ({ title: t('projects.list.pageTitle') }))
</script>

<template>
  <div class="app-page projects-page">
    <header class="app-page-head"><div><p class="app-eyebrow">{{ t('projects.list.eyebrow') }}</p><h1>{{ t('projects.list.title') }}</h1><span>{{ t('projects.list.description') }}</span></div><div v-if="canWrite('project:create') && data" class="project-create-action"><template v-if="projectLimitReached"><span>{{ projectUsage }}</span><NuxtLink class="app-button" to="/billing">{{ t('projects.list.upgradePlan') }} <ArrowUpRight :size="14" /></NuxtLink></template><button v-else class="app-button app-button--primary" @click="createOpen = true"><Plus :size="15" /> {{ t('projects.list.newProject') }}</button></div></header>
    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>{{ t('projects.list.loading') }}</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>{{ t('projects.list.unavailable') }}</h2><p role="alert">{{ apiErrorMessage(error, 'projects.list.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('projects.list.tryAgain') }}</button></div></section>
    <section v-else-if="!data?.projects.length" class="app-surface app-state"><div><FolderKanban :size="25" /><h2>{{ t('projects.list.emptyTitle') }}</h2><p>{{ t('projects.list.emptyDescription') }}</p><button v-if="canWrite('project:create') && !projectLimitReached" class="app-button app-button--primary" @click="createOpen = true"><Plus :size="15" /> {{ t('projects.list.newProject') }}</button></div></section>
    <section v-else class="project-list app-surface">
      <header><span>{{ t('projects.list.columnProject') }}</span><span>{{ t('projects.list.columnOrigins') }}</span><span>{{ t('projects.list.columnPublicKey') }}</span><span>{{ t('projects.list.columnUpdated') }}</span><span /></header>
      <NuxtLink v-for="project in data.projects" :key="project.id" :to="`/projects/${project.id}`">
        <span class="project-list-name"><i>{{ project.name.slice(0, 1).toUpperCase() }}</i><b>{{ project.name }}</b></span>
        <span>{{ project.allowed_domains.join(', ') || t('projects.list.noDomains') }}</span><code>{{ project.public_key }}</code><time>{{ formatDate(project.updated_at) }}</time><ChevronRight :size="16" />
      </NuxtLink>
    </section>

    <AppDialog :open="createOpen" aria-labelledby="create-project-title" @close="createOpen = false">
      <section class="app-modal">
        <header><div><p class="app-eyebrow">{{ t('projects.list.create.eyebrow') }}</p><h2 id="create-project-title">{{ t('projects.list.create.title') }}</h2></div><button class="app-icon-button" :title="t('projects.list.create.close')" @click="createOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="createProject"><label>{{ t('projects.list.create.name') }}<input v-model.trim="name" class="app-input" required maxlength="120" :placeholder="t('projects.list.create.namePlaceholder')" autofocus /></label><label>{{ t('projects.list.create.domains') }}<textarea v-model="domains" required placeholder="example.com&#10;*.preview.example.com" /><span>{{ t('projects.list.create.domainsHint') }}</span></label><p v-if="formError" class="project-form-error" role="alert">{{ formError }}</p><footer><button class="app-button" type="button" @click="createOpen = false">{{ t('projects.list.create.cancel') }}</button><button class="app-button app-button--primary" type="submit" :disabled="pending || !name || !parsedDomains().length">{{ pending ? t('projects.list.create.creating') : t('projects.list.create.submit') }}</button></footer></form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.project-create-action { display: flex; align-items: center; gap: 10px; }
.project-create-action > span { color: var(--muted); font-size: 10px; font-weight: 700; white-space: nowrap; }
.project-list { overflow: hidden; }
.project-list > header,
.project-list > a { display: grid; min-height: 62px; grid-template-columns: minmax(180px, 1fr) minmax(180px, 1.2fr) minmax(180px, 1fr) 90px 18px; align-items: center; gap: 14px; padding: 0 16px; border-bottom: 1px solid var(--line); }
.project-list > header { min-height: 38px; background: #f8faf9; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.project-list > a { color: var(--ink-soft); font-size: 10px; text-decoration: none; }
.project-list > a:hover { background: #f7faf9; }
.project-list > a:last-child { border-bottom: 0; }
.project-list-name { display: flex; align-items: center; gap: 10px; }
.project-list-name i { display: grid; width: 31px; height: 31px; place-items: center; background: var(--teal-soft); color: var(--teal); border-radius: 5px; font-style: normal; font-weight: 800; }
.project-list code,
.project-list > a > span:not(.project-list-name) { overflow: hidden; color: var(--muted); text-overflow: ellipsis; white-space: nowrap; }
.project-list time { color: var(--muted); font-size: 9px; }
.app-modal { width: min(520px, 100%); background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.app-modal > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.app-modal h2 { margin: 4px 0 0; font-size: 15px; }
.app-modal form { padding: 16px; }
.app-modal form label { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.app-modal textarea { min-height: 100px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 11px; }
.app-modal label span { color: var(--muted); font-size: 9px; font-weight: 500; }
.app-modal footer { display: flex; justify-content: flex-end; gap: 8px; padding-top: 8px; }
.project-form-error { padding: 9px 10px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; }
@media (max-width: 850px) { .project-create-action { align-items: flex-end; flex-direction: column; } .project-list > header { display: none; } .project-list > a { grid-template-columns: minmax(0, 1fr) 18px; padding: 10px 12px; } .project-list > a > span:not(.project-list-name), .project-list code, .project-list time { display: none; } }
</style>
