export type QueueRow = {
  name: string
  pending: number
  active: number
  scheduled: number
  retry: number
  archived: number
  failed: number
  latency: string
  selected: boolean
}

export type QueueTask = {
  id: string
  type: string
  state: string
  retries: string
  last_error: string
  next_process_at: string
}

export type QueueDashboard = {
  queues: QueueRow[]
  tasks: QueueTask[]
  selected_queue: string
  refreshed_at: string
  total_pending: number
  total_active: number
  total_retry: number
  total_failed: number
  selected_task_count: number
}

export type QueueDashboardPayload = Omit<QueueDashboard, 'queues' | 'tasks'> & {
  queues?: QueueRow[] | null
  tasks?: QueueTask[] | null
}

export function normalizeQueueDashboard(payload: QueueDashboardPayload): QueueDashboard {
  return {
    ...payload,
    queues: Array.isArray(payload.queues) ? payload.queues : [],
    tasks: Array.isArray(payload.tasks) ? payload.tasks : [],
  }
}
