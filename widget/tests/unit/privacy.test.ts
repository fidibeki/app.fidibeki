import {
  SAFE_TEXT_ATTRIBUTE,
  canonicalizeReplayURL,
  createReplayMaskTextFn,
  replayMaskTextSelector,
  sanitizeReplayEvent,
} from '../../src/modules/privacy'

describe('canonicalizeReplayURL', () => {
  it('strips credentials, query parameters, and the fragment', () => {
    const url = canonicalizeReplayURL(
      'https://sentinel-user:sentinel-pass@example.com/checkout?email=jane.doe@example.com&token=secret#fragment-secret',
    )
    expect(url).toBe('https://example.com/checkout')
  })

  it('normalizes a trailing slash but preserves the root path', () => {
    expect(canonicalizeReplayURL('https://example.com/checkout/')).toBe('https://example.com/checkout')
    expect(canonicalizeReplayURL('https://example.com/')).toBe('https://example.com/')
  })

  it('fails closed for non-HTTP(S) or malformed URLs', () => {
    expect(canonicalizeReplayURL('javascript:alert(1)')).toBeNull()
    expect(canonicalizeReplayURL('not a url')).toBeNull()
    expect(canonicalizeReplayURL('')).toBeNull()
  })
})

describe('replayMaskTextSelector', () => {
  it('matches every element so masking never depends on customer configuration', () => {
    expect(replayMaskTextSelector()).toBe('*')
    const div = document.createElement('div')
    expect(div.matches(replayMaskTextSelector())).toBe(true)
  })
})

describe('createReplayMaskTextFn', () => {
  it('masks text by default', () => {
    const maskText = createReplayMaskTextFn()
    const element = document.createElement('span')
    expect(maskText('Jane Doe', element)).toBe('**** ***')
  })

  it('masks text with no associated element', () => {
    const maskText = createReplayMaskTextFn()
    expect(maskText('jane.doe@example.com', null)).toBe('********************')
  })

  it('leaves text unmasked only inside an explicit safe opt-in region', () => {
    const maskText = createReplayMaskTextFn()
    const container = document.createElement('div')
    container.setAttribute(SAFE_TEXT_ATTRIBUTE, '')
    const child = document.createElement('span')
    container.append(child)
    expect(maskText('Powered by Fidibeki', child)).toBe('Powered by Fidibeki')
    expect(maskText('Powered by Fidibeki', container)).toBe('Powered by Fidibeki')
  })

  it('preserves whitespace so layout-relevant line breaks are unaffected', () => {
    const maskText = createReplayMaskTextFn()
    const element = document.createElement('p')
    expect(maskText('Jane\nDoe', element)).toBe('****\n***')
  })
})

describe('sanitizeReplayEvent', () => {
  it('canonicalizes the internal Meta event href', () => {
    const event = { type: 4, timestamp: 1, data: { href: 'https://user:pass@example.com/dashboard?token=secret#frag' } }
    const sanitized = sanitizeReplayEvent(event) as typeof event
    expect(sanitized?.data.href).toBe('https://example.com/dashboard')
  })

  it('drops a Meta event whose href cannot be canonicalized', () => {
    const event = { type: 4, timestamp: 1, data: { href: 'javascript:alert(1)' } }
    expect(sanitizeReplayEvent(event)).toBeNull()
  })

  it('strips non-allowlisted attributes from a full snapshot node tree', () => {
    const event = {
      type: 2,
      timestamp: 1,
      data: {
        node: {
          type: 0,
          childNodes: [
            {
              type: 2,
              tagName: 'img',
              attributes: {
                id: 'user-avatar-42', class: 'avatar', src: 'https://cdn.example.com/avatars/jane.doe@example.com.jpg',
                alt: 'Jane Doe', title: 'Jane Doe', 'aria-label': 'Jane Doe', 'data-user-id': '42', width: '80',
              },
              childNodes: [
                { type: 3, textContent: 'sentinel text left unmasked by rrweb would leak here' },
              ],
            },
          ],
        },
      },
    }
    const sanitized = sanitizeReplayEvent(event) as {
      data: { node: { childNodes: Array<{ attributes: Record<string, unknown> }> } }
    }
    const attributes = sanitized.data.node.childNodes[0]!.attributes
    expect(attributes).toEqual({ class: 'avatar', width: '80' })
    expect(Object.keys(attributes)).not.toContain('id')
    expect(Object.keys(attributes)).not.toContain('src')
    expect(Object.keys(attributes)).not.toContain('data-user-id')
  })

  it('blanks HTML comment text, which rrweb text-masking does not cover', () => {
    const event = {
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 0,
          childNodes: [{ type: 5, textContent: 'reporter: jane.doe@example.com' }],
        },
      },
    }
    const sanitized = sanitizeReplayEvent(event) as {
      data: { node: { childNodes: Array<{ textContent: string }> } }
    }
    expect(sanitized.data.node.childNodes[0]!.textContent).toBe('')
  })

  it('strips non-allowlisted attributes from incremental attribute mutations', () => {
    const event = {
      type: 3, timestamp: 1,
      data: {
        source: 0,
        attributes: [{ id: 7, attributes: { id: 'user-42', title: 'Jane Doe', style: 'color:red' } }],
        texts: [], adds: [], removes: [],
      },
    }
    const sanitized = sanitizeReplayEvent(event) as {
      data: { attributes: Array<{ attributes: Record<string, unknown> }> }
    }
    expect(sanitized.data.attributes[0]!.attributes).toEqual({ style: 'color:red' })
  })

  it('sanitizes newly-added node subtrees in incremental mutations', () => {
    const event = {
      type: 3, timestamp: 1,
      data: {
        source: 0,
        attributes: [], texts: [], removes: [],
        adds: [{
          parentId: 1, nextId: null,
          node: {
            type: 2, tagName: 'div',
            attributes: { 'data-reporter-email': 'jane.doe@example.com', class: 'card' },
            childNodes: [],
          },
        }],
      },
    }
    const sanitized = sanitizeReplayEvent(event) as {
      data: { adds: Array<{ node: { attributes: Record<string, unknown> } }> }
    }
    expect(sanitized.data.adds[0]!.node.attributes).toEqual({ class: 'card' })
  })

  it('passes through incremental sources unrelated to mutations unchanged', () => {
    const event = { type: 3, timestamp: 1, data: { source: 1, positions: [{ x: 1, y: 2 }] } }
    expect(sanitizeReplayEvent(event)).toEqual(event)
  })

  it('passes through unrecognized event shapes unchanged', () => {
    const event = { type: 5, timestamp: 1, data: { tag: 'custom', payload: { foo: 'bar' } } }
    expect(sanitizeReplayEvent(event)).toEqual(event)
  })

  it('fails closed for non-object input', () => {
    expect(sanitizeReplayEvent(null)).toBeNull()
    expect(sanitizeReplayEvent(undefined)).toBeNull()
    expect(sanitizeReplayEvent('not an event')).toBeNull()
  })

  it('fails closed when sanitization throws', () => {
    const throwing = {
      type: 2,
      timestamp: 1,
      get data(): unknown {
        throw new Error('malformed event')
      },
    }
    expect(sanitizeReplayEvent(throwing)).toBeNull()
  })
})
