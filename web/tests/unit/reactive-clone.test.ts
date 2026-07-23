import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import { cloneReactive } from '../../utils/reactive-clone'

describe('cloneReactive', () => {
  it('clones API data after Vue wraps it in a reactive proxy', () => {
    const localizations = reactive({
      'pt-ao': {
        prompt: 'Como foi a compra?',
        block_labels: { feedback: 'Conte-nos mais' },
      },
    })

    const cloned = cloneReactive(localizations)

    expect(cloned).toEqual({
      'pt-ao': {
        prompt: 'Como foi a compra?',
        block_labels: { feedback: 'Conte-nos mais' },
      },
    })
    expect(cloned).not.toBe(localizations)
    expect(cloned['pt-ao']).not.toBe(localizations['pt-ao'])
  })
})
