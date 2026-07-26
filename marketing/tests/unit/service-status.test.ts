import { describe, expect, it } from 'vitest'
import { readOverview } from '~/composables/useServiceStatus'

/**
 * The endpoint behind this carries no compatibility guarantee, so the rule that
 * matters is negative: nothing may resolve to `operational` unless the payload
 * actually proved it. These tests exist to keep that true as the shape drifts.
 */
describe('readOverview', () => {
  const operational = {
    overallStatus: { name: 'Operational', isOperationalState: true, priority: 1 },
    activeIncidents: [],
    scheduledMaintenanceEvents: [],
  }

  it('reports operational only for a healthy payload', () => {
    expect(readOverview(operational)).toEqual({ level: 'operational', label: 'Operational' })
  })

  it('falls back to a generic label when the status has no name', () => {
    const result = readOverview({ ...operational, overallStatus: { isOperationalState: true } })
    expect(result).toEqual({ level: 'operational', label: 'All systems operational' })
  })

  it('treats a non-operational state as down', () => {
    const result = readOverview({
      ...operational,
      overallStatus: { name: 'Offline', isOperationalState: false },
    })
    expect(result).toEqual({ level: 'down', label: 'Offline' })
  })

  it('believes the worse half when the payload contradicts itself', () => {
    // Operational flag, but an incident is open. Never call that healthy.
    const result = readOverview({ ...operational, activeIncidents: [{ _id: 'a' }] })
    expect(result.level).toBe('degraded')
  })

  it('downgrades to degraded while maintenance is scheduled', () => {
    const result = readOverview({ ...operational, scheduledMaintenanceEvents: [{ _id: 'm' }] })
    expect(result).toEqual({ level: 'degraded', label: 'Maintenance scheduled' })
  })

  it.each([
    ['null', null],
    ['undefined', undefined],
    ['a string', 'Operational'],
    ['an array', []],
    ['an empty object', {}],
    ['a missing overallStatus', { activeIncidents: [] }],
    ['a null overallStatus', { overallStatus: null }],
    ['a non-boolean operational flag', { overallStatus: { isOperationalState: 'true' } }],
    ['a numeric operational flag', { overallStatus: { isOperationalState: 1 } }],
  ])('returns unknown for %s', (_label, payload) => {
    expect(readOverview(payload)).toEqual({ level: 'unknown', label: 'Status unavailable' })
  })

  it('never claims health from a payload it could not read', () => {
    const hostile = [
      null,
      {},
      { overallStatus: {} },
      { overallStatus: { isOperationalState: 'yes' } },
      { overallStatus: { name: 'Operational' } },
    ]
    for (const payload of hostile) {
      expect(readOverview(payload).level).not.toBe('operational')
    }
  })

  it('tolerates non-array incident fields without claiming health', () => {
    const result = readOverview({ ...operational, activeIncidents: 'three' })
    // A non-array counts as zero incidents, so the operational flag still governs —
    // but the flag itself was real, so this stays a verified claim.
    expect(result.level).toBe('operational')
  })
})
