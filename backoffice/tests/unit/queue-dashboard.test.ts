import { describe, expect, it } from 'vitest'
import { normalizeQueueDashboard } from '../../utils/queue-dashboard'

describe('normalizeQueueDashboard', () => {
  it('normalizes legacy null collections to empty arrays', () => {
    const dashboard = normalizeQueueDashboard({
      queues: null,
      tasks: null,
      selected_queue: 'email-pool',
      refreshed_at: '2026-07-28 11:00:00 UTC',
      total_pending: 0,
      total_active: 0,
      total_retry: 0,
      total_failed: 0,
      selected_task_count: 0,
    })

    expect(dashboard.queues).toEqual([])
    expect(dashboard.tasks).toEqual([])
  })
})
