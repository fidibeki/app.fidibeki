import { describe, expect, it } from 'vitest'
import {
  replaySessionDayBreaks,
  replaySessionDurationSeconds,
  replaySessionListQuery,
  replaySessionStatusClass,
} from '~/utils/replay-sessions'

describe('replaySessionListQuery', () => {
  it('always pins the page size', () => {
    expect(replaySessionListQuery({})).toBe('limit=50')
  })

  it('serializes every active filter', () => {
    expect(replaySessionListQuery({
      projectId: '7d4a51dd-3f3a-4a4e-9c5d-14f1de8a3b9b',
      status: 'ready',
      feedback: 'unlinked',
    })).toBe('project_id=7d4a51dd-3f3a-4a4e-9c5d-14f1de8a3b9b&status=ready&feedback=unlinked&limit=50')
  })

  it('omits empty filters and appends the cursor last', () => {
    expect(replaySessionListQuery({ projectId: '', status: '', feedback: '', cursor: 'abc' }))
      .toBe('limit=50&cursor=abc')
  })
})

describe('replaySessionDurationSeconds', () => {
  it('measures ended sessions', () => {
    expect(replaySessionDurationSeconds(
      '2026-08-01T10:00:00Z', '2026-08-01T10:05:30Z',
    )).toBe(330)
  })

  it('is null while the session is still open', () => {
    expect(replaySessionDurationSeconds('2026-08-01T10:00:00Z', null)).toBeNull()
  })

  it('never reports negative durations', () => {
    expect(replaySessionDurationSeconds(
      '2026-08-01T10:00:00Z', '2026-08-01T09:59:00Z',
    )).toBe(0)
  })
})

describe('replaySessionDayBreaks', () => {
  const labelFor = (startedAt: string) => startedAt.slice(0, 10)

  it('marks the first session of each day run', () => {
    const breaks = replaySessionDayBreaks([
      { id: 'a', started_at: '2026-08-05T10:00:00Z' },
      { id: 'b', started_at: '2026-08-05T08:00:00Z' },
      { id: 'c', started_at: '2026-08-04T22:00:00Z' },
      { id: 'd', started_at: '2026-08-04T09:00:00Z' },
      { id: 'e', started_at: '2026-08-01T09:00:00Z' },
    ], labelFor)
    expect([...breaks.entries()]).toEqual([
      ['a', '2026-08-05'],
      ['c', '2026-08-04'],
      ['e', '2026-08-01'],
    ])
  })

  it('is empty for an empty list', () => {
    expect(replaySessionDayBreaks([], labelFor).size).toBe(0)
  })
})

describe('replaySessionStatusClass', () => {
  it('maps each status onto an existing pill class', () => {
    expect(replaySessionStatusClass('ready')).toBe('status-completed')
    expect(replaySessionStatusClass('recording')).toBe('status-processing')
    expect(replaySessionStatusClass('processing')).toBe('status-processing')
    expect(replaySessionStatusClass('failed')).toBe('status-failed')
    expect(replaySessionStatusClass('expired')).toBe('status-expired')
  })
})
