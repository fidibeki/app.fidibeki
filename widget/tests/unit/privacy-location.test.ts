import privacyLocationContract from '../../../contracts/privacy-location.json'
import { privacySafeLocation } from '../../src/privacy-location'
import type { WidgetRule } from '../../src/types'

describe('privacy-safe location contract', () => {
  it.each(privacyLocationContract.projections)('projects $name', (testCase) => {
    const result = privacySafeLocation(testCase.url, testCase.rule as WidgetRule)
    if (!testCase.valid) {
      expect(result).toBeNull()
      return
    }
    expect(result).toEqual({ target: testCase.target, source: testCase.source })
  })
})
