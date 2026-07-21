<script setup lang="ts">
import { AlertTriangle, ChevronRight, FolderKanban, Plus, X } from '@lucide/vue'
import type { Project } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDate } from '~/utils/format'

const { $apiFetch } = useNuxtApp()
const { can } = useAuth()
const createOpen = ref(false)
const name = ref('')
const domains = ref('')
const pending = ref(false)
const formError = ref('')

const { data, status, error, refresh } = await useAsyncData(
  'projects-list',
  () => $apiFetch<{ projects: Project[] }>('/v1/projects'),
)

function parsedDomains() {
  return domains.value.split(/[\n,]+/).map((domain) => domain.trim()).filter(Boolean)
}

async function createProject() {
  if (pending.value) return
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
    formError.value = apiErrorMessage(createError, 'Project could not be created.')
  } finally { pending.value = false }
}

useHead({ title: 'Projects · Fidibeki' })
</script>

<template>
  <div class="app-page projects-page">
    <header class="app-page-head"><div><p class="app-eyebrow">Collection</p><h1>Projects</h1><span>Manage widget origins, rules, and public embed keys.</span></div><button v-if="can('project:create')" class="app-button app-button--primary" @click="createOpen = true"><Plus :size="15" /> New project</button></header>
    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>Loading projects</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>Projects unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div></section>
    <section v-else-if="!data?.projects.length" class="app-surface app-state"><div><FolderKanban :size="25" /><h2>No projects yet</h2><p>Create a project to receive a scoped public key and configure the feedback widget.</p><button v-if="can('project:create')" class="app-button app-button--primary" @click="createOpen = true"><Plus :size="15" /> New project</button></div></section>
    <section v-else class="project-list app-surface">
      <header><span>Project</span><span>Allowed origins</span><span>Public key</span><span>Updated</span><span /></header>
      <NuxtLink v-for="project in data.projects" :key="project.id" :to="`/projects/${project.id}`">
        <span class="project-list-name"><i>{{ project.name.slice(0, 1).toUpperCase() }}</i><b>{{ project.name }}</b></span>
        <span>{{ project.allowed_domains.join(', ') || 'No domains' }}</span><code>{{ project.public_key }}</code><time>{{ formatDate(project.updated_at) }}</time><ChevronRight :size="16" />
      </NuxtLink>
    </section>

    <AppDialog :open="createOpen" aria-labelledby="create-project-title" @close="createOpen = false">
      <section class="app-modal">
        <header><div><p class="app-eyebrow">Collection endpoint</p><h2 id="create-project-title">Create project</h2></div><button class="app-icon-button" title="Close" @click="createOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="createProject"><label>Project name<input v-model.trim="name" class="app-input" required maxlength="120" placeholder="Storefront" autofocus /></label><label>Allowed domains<textarea v-model="domains" required placeholder="example.com&#10;*.preview.example.com" /><span>One exact hostname or leading wildcard per line.</span></label><p v-if="formError" class="project-form-error" role="alert">{{ formError }}</p><footer><button class="app-button" type="button" @click="createOpen = false">Cancel</button><button class="app-button app-button--primary" type="submit" :disabled="pending || !name || !parsedDomains().length">{{ pending ? 'Creating…' : 'Create project' }}</button></footer></form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
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
@media (max-width: 850px) { .project-list > header { display: none; } .project-list > a { grid-template-columns: minmax(0, 1fr) 18px; padding: 10px 12px; } .project-list > a > span:not(.project-list-name), .project-list code, .project-list time { display: none; } }
</style>
