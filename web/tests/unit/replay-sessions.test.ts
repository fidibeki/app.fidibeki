import { describe, expect, it } from 'vitest'
import {
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

describe('replaySessionStatusClass', () => {
  it('maps each status onto an existing pill class', () => {
    expect(replaySessionStatusClass('ready')).toBe('status-completed')
    expect(replaySessionStatusClass('recording')).toBe('status-processing')
    expect(replaySessionStatusClass('processing')).toBe('status-processing')
    expect(replaySessionStatusClass('failed')).toBe('status-failed')
    expect(replaySessionStatusClass('expired')).toBe('status-expired')
  })
})
