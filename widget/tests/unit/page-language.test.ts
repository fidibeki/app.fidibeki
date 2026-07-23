import { subscribeToPageLanguage } from '../../src/page-language'

describe('page language subscription', () => {
  afterEach(() => {
    document.documentElement.lang = ''
  })

  it('normalizes root language changes and shares delivery across subscribers', async () => {
    const first: Array<string | null> = []
    const second: Array<string | null> = []
    const unsubscribeFirst = subscribeToPageLanguage(language => first.push(language), document)
    const unsubscribeSecond = subscribeToPageLanguage(language => second.push(language), document)

    document.documentElement.lang = 'pt-AO'
    await mutationDelivery()

    expect(first).toEqual(['pt-ao'])
    expect(second).toEqual(['pt-ao'])

    unsubscribeFirst()
    document.documentElement.lang = 'en'
    await mutationDelivery()

    expect(first).toEqual(['pt-ao'])
    expect(second).toEqual(['pt-ao', 'en'])

    unsubscribeSecond()
    document.documentElement.lang = 'pt'
    await mutationDelivery()
    expect(second).toEqual(['pt-ao', 'en'])
  })

  it('reports an unusable root language as absent', async () => {
    document.documentElement.lang = 'en'
    const languages: Array<string | null> = []
    const unsubscribe = subscribeToPageLanguage(language => languages.push(language), document)

    document.documentElement.lang = 'not_a_locale!'
    await mutationDelivery()

    expect(languages).toEqual([null])
    unsubscribe()
  })
})

async function mutationDelivery(): Promise<void> {
  await new Promise(resolve => setTimeout(resolve, 0))
}
