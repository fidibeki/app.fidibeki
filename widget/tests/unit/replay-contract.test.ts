import { sanitizeReplayEvent } from '../../src/modules/privacy'
import rawContract from '../../../contracts/replay-events.json'

interface ReplayEventContract {
  page_url: string
  events: Array<{ name: string; event: unknown }>
}

const contract = rawContract as ReplayEventContract

describe('replay event contract', () => {
  it.each(contract.events)('sanitizer passes "$name" through unchanged', ({ event }) => {
    const wire = structuredClone(event)
    const sanitized = sanitizeReplayEvent(structuredClone(event), contract.page_url)
    expect(sanitized).not.toBeNull()
    // Contract events are in final wire form: sanitization must be a no-op, because this
    // exact JSON is what the backend verifier (contract_test.go) promises to accept.
    expect(sanitized).toEqual(wire)
  })
})
