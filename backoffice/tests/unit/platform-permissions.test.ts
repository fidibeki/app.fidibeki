import { describe, expect, it } from 'vitest'
import { canRunQueueTasks, platformQueueRunPermission } from '~/utils/platform-permissions'

describe('canRunQueueTasks', () => {
  it('only enables queue retry for the explicit command permission', () => {
    expect(canRunQueueTasks(undefined)).toBe(false)
    expect(canRunQueueTasks([])).toBe(false)
    expect(canRunQueueTasks(['platform:tenant:read'])).toBe(false)
    expect(canRunQueueTasks([platformQueueRunPermission])).toBe(true)
  })
})
