import { createReplayMaskTextFn, sanitizeReplayEvent } from '../../src/modules/privacy'
import { sanitizeReplayCSS, safeReplayAssetURL } from '../../src/modules/replay-content'

describe('replay fidelity with selective privacy', () => {
  it('keeps UI labels readable while hiding personal information', () => {
    const mask = createReplayMaskTextFn()
    const label = document.createElement('button')
    expect(mask('Save changes', label)).toBe('Save changes')
    expect(mask('Contact jane@example.com', label)).toBe('Contact ****************')
    const privateRegion = document.createElement('div')
    privateRegion.setAttribute('data-fidibeki-mask', '')
    privateRegion.append(label)
    expect(mask('Jane Doe', label)).toBe('**** ***')
  })

  it('preserves native stylesheet, layout selectors, and SVG geometry', () => {
    const event = { type: 2, timestamp: 1, data: { node: {
      type: 2, id: 1, tagName: 'main', attributes: { class: 'dashboard', id: 'app' },
      childNodes: [
        { type: 2, id: 2, tagName: 'style', attributes: { _cssText: '.dashboard { display: flex; border: 0; }' }, childNodes: [] },
        { type: 2, id: 3, tagName: 'svg', isSVG: true, attributes: { viewBox: '0 0 24 24', fill: 'none' }, childNodes: [
          { type: 2, id: 4, tagName: 'path', isSVG: true, attributes: { d: 'M2 12h20', stroke: 'currentColor' }, childNodes: [] },
        ] },
        { type: 3, id: 5, textContent: 'Save changes' },
      ],
    } } }
    const result = sanitizeReplayEvent(event, 'https://example.com/') as typeof event
    expect(result.data.node.attributes).toEqual({ class: 'dashboard', id: 'app' })
    expect(JSON.stringify(result)).toContain('display: flex')
    expect(JSON.stringify(result)).toContain('M2 12h20')
    expect(JSON.stringify(result)).toContain('Save changes')
  })

  it('masks configured private content in shadow roots and ignores safe-text overrides', () => {
    const host = document.createElement('div')
    host.className = 'private-account'
    const shadow = host.attachShadow({ mode: 'open' })
    const name = document.createElement('span')
    name.setAttribute('data-fidibeki-safe-text', '')
    shadow.append(name)
    expect(createReplayMaskTextFn('.private-account')('Jane Doe', name)).toBe('**** ***')
    expect(createReplayMaskTextFn('[')('Jane Doe', name)).toBe('**** ***')
  })

  it('redacts contact and payment numbers without masking ordinary counts', () => {
    const mask = createReplayMaskTextFn()
    expect(mask('Call +351 912 345 678', null)).toBe('Call **** *** *** ***')
    expect(mask('Card 4242 4242 4242 4242', null)).toBe('Card **** **** **** ****')
    expect(mask('Showing 1 to 20 of 80', null)).toBe('Showing 1 to 20 of 80')
    expect(mask('Created 2026-10-02', null)).toBe('Created 2026-10-02')
  })

  it('keeps static assets but rejects private and signed resource URLs', () => {
    expect(safeReplayAssetURL('https://cdn.example.com/assets/logo.svg')).toBe(true)
    expect(safeReplayAssetURL('https://cdn.example.com/fonts/inter.woff2')).toBe(true)
    for (const url of ['https://cdn.example.com/avatar/jane.png', 'https://cdn.example.com/assets/jane%40example.com.png', 'https://cdn.example.com/logo.svg?token=secret', 'data:image/png;base64,secret']) {
      expect(safeReplayAssetURL(url)).toBe(false)
    }
  })

  it('preserves CSS variables and fonts while removing private URLs and generated content', () => {
    const css = sanitizeReplayCSS(`
      /* personal-secret */
      @font-face { font-family: Inter; src: url("https://cdn.example.com/fonts/inter.woff2"); }
      .card { --accent: red; color: var(--accent); background-image: url("https://example.com/avatar/jane.png"); }
      .card::before { content: "Jane Doe"; }
      .card::after { content: ""; border: 1px solid red; }
    `)
    expect(css).toContain('inter.woff2')
    expect(css).toContain('var(--accent)')
    expect(css).not.toContain('personal-secret')
    expect(css).not.toContain('Jane Doe')
    expect(css).not.toContain('/avatar/')
    expect(css).toContain('content: ""')
  })

  it('masks private incremental additions and text changes using live node ancestry', () => {
    const parent = document.createElement('div')
    parent.setAttribute('data-fidibeki-mask', '')
    const text = document.createTextNode('Jane Doe')
    parent.append(text)
    const event = { type: 3, timestamp: 1, data: { source: 0,
      adds: [{ parentId: 1, nextId: null, node: { type: 3, id: 3, textContent: 'Jane Doe' } }],
      texts: [{ id: 2, value: 'Jane Doe' }], attributes: [], removes: [],
    } }
    const output = sanitizeReplayEvent(event, 'https://example.com/', id => id === 1 ? parent : text)
    expect(JSON.stringify(output)).not.toContain('Jane Doe')
    expect(JSON.stringify(output)).toContain('**** ***')
  })

  it('preserves numeric CSS text mutations through the rrweb text hook and sanitizer', () => {
    const style = document.createElement('style')
    const text = document.createTextNode('.card { opacity: 0.12345678; }')
    style.append(text)
    const value = createReplayMaskTextFn()(text.textContent!, style)
    const output = sanitizeReplayEvent({ type: 3, timestamp: 1, data: {
      source: 0, texts: [{ id: 2, value }],
    } }, 'https://example.com/', () => text)
    expect(JSON.stringify(output)).toContain('opacity: 0.12345678')
  })
})
