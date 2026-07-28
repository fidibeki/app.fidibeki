<script setup lang="ts">
type Tenant = { id: string; name: string; slug: string; plan: string; status: string; created_at: string }
type Page = { tenants: Tenant[]; next_cursor?: string }

const route = useRoute()
const router = useRouter()
const { $platformApi } = useNuxtApp()
const query = ref(String(route.query.q || ''))
const status = ref(String(route.query.status || ''))
const plan = ref(String(route.query.plan || ''))
const appliedQuery = ref(query.value)
const appliedStatus = ref(status.value)
const appliedPlan = ref(plan.value)
const cursor = ref('')
const nextCursor = ref('')
const tenants = ref<Tenant[]>([])
const request = computed(() => {
  const parameters = new URLSearchParams({ q: appliedQuery.value, status: appliedStatus.value, plan: appliedPlan.value, limit: '25' })
  if (cursor.value) parameters.set('cursor', cursor.value)
  return `/admin/backoffice/tenants?${parameters}`
})
const { data, pending } = await useAsyncData('workspaces', () => $platformApi<Page>(request.value), { server: false, watch: [request] })
watch(data, (page) => {
  if (!page) return
  tenants.value = cursor.value ? [...tenants.value, ...page.tenants] : page.tenants
  nextCursor.value = page.next_cursor || ''
}, { immediate: true })
async function submit() {
  // Only submitted values affect the query. This prevents a draft edit from
  // reusing a page-two cursor and mixing result sets while an operator types.
  appliedQuery.value = query.value; appliedStatus.value = status.value; appliedPlan.value = plan.value
  cursor.value = ''
  await router.replace({ query: { ...(query.value && { q: query.value }), ...(status.value && { status: status.value }), ...(plan.value && { plan: plan.value }) } })
}
function loadMore() { if (nextCursor.value && !pending.value) cursor.value = nextCursor.value }
</script>

<template>
  <section>
    <header class="page-head"><div><p class="eyebrow">Workspace directory</p><h1 class="title">Every account,<br>in context.</h1></div><NuxtLink class="button" to="/workspaces/new">New workspace</NuxtLink></header>
    <form class="panel filter-form" @submit.prevent="submit">
      <input v-model="query" placeholder="Search name or slug" aria-label="Search workspaces">
      <select v-model="status" aria-label="Workspace status"><option value="">All statuses</option><option>active</option><option>trial</option><option>suspended</option><option>cancelled</option></select>
      <select v-model="plan" aria-label="Workspace plan"><option value="">All plans</option><option>starter</option><option>growth</option><option>scale</option><option>enterprise</option></select>
      <button class="button" type="submit">Filter</button>
    </form>
    <section class="panel result-panel">
      <div class="table-wrap"><table class="table"><thead><tr><th>Workspace</th><th>Plan</th><th>Status</th><th>Created</th></tr></thead><tbody><tr v-for="tenant in tenants" :key="tenant.id"><td><NuxtLink :to="`/workspaces/${tenant.id}`">{{ tenant.name }}<br><span class="muted">{{ tenant.slug }}</span></NuxtLink></td><td>{{ tenant.plan }}</td><td>{{ tenant.status }}</td><td>{{ new Date(tenant.created_at).toLocaleDateString() }}</td></tr></tbody></table></div>
      <button v-if="nextCursor" class="button result-panel__action" :disabled="pending" @click="loadMore">{{ pending ? 'Loading…' : 'Load more workspaces' }}</button>
    </section>
  </section>
</template>
