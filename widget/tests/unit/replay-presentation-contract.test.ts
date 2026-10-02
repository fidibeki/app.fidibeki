import contract from '../../../contracts/replay-presentation.json'
import { sanitizeReplayCSS } from '../../src/modules/replay-content'

describe('native replay presentation contract', () => {
  it.each(contract.valid)('preserves "$name"', ({ style, inline }) => {
    expect(sanitizeReplayCSS(style, inline)).not.toBe('')
  })

  it.each(contract.invalid)('removes "$name"', ({ style, inline }) => {
    expect(sanitizeReplayCSS(style, inline)).toBe('')
  })

  it('limits stylesheet size', () => {
    expect(sanitizeReplayCSS('x'.repeat(1024 * 1024 + 1))).toBe('')
  })
})
