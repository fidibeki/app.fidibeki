import contractJSON from '../../../contracts/replay-presentation.json'
import { isValidReplayPresentationStyle } from '../../src/modules/privacy'

interface ReplayPresentationContract {
  valid: Array<{ name: string; style: string }>
  invalid: Array<{ name: string; style: string }>
}

const contract = contractJSON as ReplayPresentationContract

describe('replay presentation contract', () => {
  it.each(contract.valid)('accepts "$name"', ({ style }) => {
    expect(isValidReplayPresentationStyle(style)).toBe(true)
  })

  it.each(contract.invalid)('rejects "$name"', ({ style }) => {
    expect(isValidReplayPresentationStyle(style)).toBe(false)
  })

  it('rejects presentation values and complete styles above their byte limits', () => {
    expect(isValidReplayPresentationStyle(`grid-template-columns:${'1px '.repeat(70)}`)).toBe(false)
    expect(isValidReplayPresentationStyle('x'.repeat(4 * 1024 + 1))).toBe(false)
  })
})
