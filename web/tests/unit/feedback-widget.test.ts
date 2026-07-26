import { describe, expect, it } from 'vitest'
import { loaderScriptURL, mountFeedbackWidget } from '../../utils/feedback-widget'

const KEY = 'pk_live_GjFasO9215CNAax7LbWhiP'

interface FakeScript {
  async: boolean
  src: string
  dataset: Record<string, string>
}

/**
 * A stand-in for the four DOM calls the function makes.
 *
 * The rest of this suite runs in the node environment and has no DOM
 * dependency; adding one so that ten lines of script tag can be exercised would
 * cost more than it proves. mountFeedbackWidget takes its Document as an
 * argument precisely so this is possible.
 */
function fakeDocument() {
  const added: FakeScript[] = []
  const document = {
    createElement: (): FakeScript => ({ async: false, src: '', dataset: {} }),
    querySelector: (selector: string) =>
      selector === 'script[data-fidibeki-key]'
        ? added.find(script => script.dataset.fidibekiKey) ?? null
        : null,
    head: { append: (script: FakeScript) => { added.push(script) } },
  }
  return { document: document as unknown as Document, added }
}

describe('loaderScriptURL', () => {
  it('builds the loader path on the configured host', () => {
    expect(loaderScriptURL('https://widget.fidibeki.com', KEY))
      .toBe('https://widget.fidibeki.com/v1/loader.js')
  })

  it('does not double the slash when the host carries one', () => {
    expect(loaderScriptURL('https://widget.fidibeki.com/', KEY))
      .toBe('https://widget.fidibeki.com/v1/loader.js')
  })

  // Both halves are configuration. A deployment that has not set them, or has
  // deliberately blanked the key, must get no script at all rather than one
  // pointing at /v1/loader.js on the app's own origin, which serves the app.
  it('declines when either half is missing', () => {
    expect(loaderScriptURL('', KEY)).toBe('')
    expect(loaderScriptURL('https://widget.fidibeki.com', '')).toBe('')
    expect(loaderScriptURL('   ', '  ')).toBe('')
  })
})

describe('mountFeedbackWidget', () => {
  it('adds one async script carrying the public key', () => {
    const { document, added } = fakeDocument()
    expect(mountFeedbackWidget(document, 'https://widget.fidibeki.com', KEY)).toBe(true)
    expect(added).toHaveLength(1)
    expect(added[0].src).toBe('https://widget.fidibeki.com/v1/loader.js')
    expect(added[0].dataset.fidibekiKey).toBe(KEY)
    // Async, because the backoffice must not wait on our own widget to render.
    expect(added[0].async).toBe(true)
  })

  // The layout mounts again on client-side navigation, so this runs more than
  // once per page load.
  it('does not add a second loader', () => {
    const { document, added } = fakeDocument()
    expect(mountFeedbackWidget(document, 'https://widget.fidibeki.com', KEY)).toBe(true)
    expect(mountFeedbackWidget(document, 'https://widget.fidibeki.com', KEY)).toBe(false)
    expect(added).toHaveLength(1)
  })

  it('adds nothing when the widget is switched off', () => {
    const { document, added } = fakeDocument()
    expect(mountFeedbackWidget(document, 'https://widget.fidibeki.com', '')).toBe(false)
    expect(added).toHaveLength(0)
  })
})
