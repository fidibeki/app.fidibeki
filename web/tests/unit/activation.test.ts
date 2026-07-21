import { describe, expect, it } from 'vitest'
import { canActivateWorkspace } from '../../utils/activation'

describe('workspace activation permissions', () => {
  it('requires both project permissions and widget rule creation', () => {
    expect(canActivateWorkspace(['project:create', 'project:read', 'widget_rule:create'])).toBe(true)
    expect(canActivateWorkspace(['project:create', 'project:read'])).toBe(false)
    expect(canActivateWorkspace(['project:read', 'widget_rule:create'])).toBe(false)
  })
})
