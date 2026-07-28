<script setup lang="ts">
import { canRunQueueTasks } from '~/utils/platform-permissions'

type Queue = { name: string; pending: number; active: number; scheduled: number; retry: number; archived: number; failed: number; latency: string; selected: boolean }
type Task = { id: string; type: string; state: string; retries: string; last_error: string; next_process_at: string }
type Dashboard = { queues: Queue[]; tasks: Task[]; selected_queue: string; refreshed_at: string; total_pending: number; total_active: number; total_retry: number; total_failed: number; selected_task_count: number }

const route = useRoute()
const { $platformApi } = useNuxtApp()
const { session } = usePlatformAuth()
const mayRetryTasks = computed(() => canRunQueueTasks(session.value.admin?.permissions))
const selectedQueue = ref(typeof route.query.queue === 'string' ? route.query.queue : '')
const request = computed(() => selectedQueue.value ? `/admin/backoffice/queues?queue=${encodeURIComponent(selectedQueue.value)}` : '/admin/backoffice/queues')
const { data, error, pending, refresh } = await useAsyncData('queue-dashboard', () => $platformApi<{ dashboard: Dashboard }>(request.value), { server: false, watch: [request] })
const retryingTaskID = ref('')
const retryError = ref('')

function selectQueue(queue: string) {
  if (queue === data.value?.dashboard.selected_queue) return
  selectedQueue.value = queue
  if (import.meta.client) window.history.replaceState(window.history.state, '', `${route.path}?queue=${encodeURIComponent(queue)}`)
}

function statusTone(state: string) {
  return state === 'retry' || state === 'archived' ? 'warning' : state === 'active' ? 'complete' : ''
}

function refreshQueues() { return refresh() }

async function retryTask(task: Task) {
  const queue = data.value?.dashboard.selected_queue
  if (!mayRetryTasks.value || !queue || task.state !== 'retry' || retryingTaskID.value) return
  retryingTaskID.value = task.id
  retryError.value = ''
  try {
    await $platformApi(`/admin/backoffice/queues/${encodeURIComponent(queue)}/tasks/${encodeURIComponent(task.id)}/retry`, { method: 'POST' })
    await refresh()
  } catch {
    retryError.value = 'The task was not retried. Check its current state and try again.'
  } finally {
    retryingTaskID.value = ''
  }
}
</script>

<template>
  <section>
    <header class="page-head">
      <div><p class="eyebrow">Platform operations</p><h1 class="title">Queue operations</h1><p class="subtitle">Inspect background work and explicitly retry failed delivery tasks.</p></div>
      <button class="button secondary" type="button" :disabled="pending" @click="refreshQueues">{{ pending ? 'Refreshing…' : 'Refresh' }}</button>
    </header>

    <p v-if="error" class="error" role="alert">Queue data is temporarily unavailable. Nothing was changed.</p>
    <template v-else-if="data">
      <div class="metrics">
        <article class="metric"><b>{{ data.dashboard.total_pending }}</b><span>Pending</span></article>
        <article class="metric"><b>{{ data.dashboard.total_active }}</b><span>Active</span></article>
        <article class="metric"><b>{{ data.dashboard.total_retry }}</b><span>Retrying</span></article>
        <article class="metric"><b>{{ data.dashboard.total_failed }}</b><span>Failed</span></article>
      </div>

      <section class="panel"><p class="eyebrow">Queues</p><div v-if="data.dashboard.queues.length" class="table-wrap"><table class="table"><thead><tr><th>Queue</th><th>Pending</th><th>Active</th><th>Scheduled</th><th>Retry</th><th>Archived</th><th>Latency</th></tr></thead><tbody><tr v-for="queue in data.dashboard.queues" :key="queue.name" :class="{ 'table-row-selected': queue.selected }"><td><button class="queue-link" :class="{ 'queue-link--selected': queue.selected }" type="button" :aria-current="queue.selected ? 'page' : undefined" @click="selectQueue(queue.name)">{{ queue.name }}</button></td><td>{{ queue.pending }}</td><td>{{ queue.active }}</td><td>{{ queue.scheduled }}</td><td>{{ queue.retry }}</td><td>{{ queue.archived }}</td><td>{{ queue.latency }}</td></tr></tbody></table></div><p v-else class="empty-state">No queues have been created yet.</p></section>

      <section class="panel"><p class="eyebrow">Tasks</p><h2>{{ data.dashboard.selected_queue || 'No queue selected' }} <span class="muted">· {{ data.dashboard.selected_task_count }} shown</span></h2><p v-if="retryError" class="error queue-action-error" role="alert">{{ retryError }}</p><div v-if="data.dashboard.tasks.length" class="table-wrap"><table class="table"><thead><tr><th>Type</th><th>State</th><th>Task ID</th><th>Retries</th><th>Last error</th><th>Next run (UTC)</th><th>Action</th></tr></thead><tbody><tr v-for="task in data.dashboard.tasks" :key="task.id"><td>{{ task.type }}</td><td><span class="status" :class="statusTone(task.state) && `status--${statusTone(task.state)}`">{{ task.state }}</span></td><td>{{ task.id }}</td><td>{{ task.retries }}</td><td>{{ task.last_error }}</td><td>{{ task.next_process_at }}</td><td><button v-if="mayRetryTasks && task.state === 'retry'" class="button secondary button--compact" type="button" :disabled="Boolean(retryingTaskID)" @click="retryTask(task)">{{ retryingTaskID === task.id ? 'Retrying…' : 'Retry now' }}</button><span v-else class="muted">—</span></td></tr></tbody></table></div><p v-else class="empty-state">No pending, active, scheduled, retry, or archived tasks in this queue.</p></section>
    </template>
    <p v-else class="empty-state">Loading queue operations…</p>
  </section>
</template>
