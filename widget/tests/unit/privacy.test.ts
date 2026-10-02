import {
  SAFE_TEXT_ATTRIBUTE,
  canonicalizeReplayURL,
  createReplayMaskTextFn,
  isUnserializedReplayEvent,
  replayMaskTextSelector,
  sanitizeReplayEvent as sanitizeReplayEventForSession,
} from '../../src/modules/privacy'

describe('isUnserializedReplayEvent', () => {
  it('flags incremental events that target mirror id -1 (blocked regions)', () => {
    expect(isUnserializedReplayEvent({
      type: 3, timestamp: 1, data: { source: 5, id: -1, text: '****', isChecked: false },
    })).toBe(true)
    expect(isUnserializedReplayEvent({
      type: 3, timestamp: 1, data: { source: 3, id: -1, x: 1, y: 2 },
    })).toBe(true)
  })

  it('leaves events with real targets, other types, and malformed input alone', () => {
    expect(isUnserializedReplayEvent({
      type: 3, timestamp: 1, data: { source: 5, id: 7, text: '****' },
    })).toBe(false)
    expect(isUnserializedReplayEvent({ type: 3, timestamp: 1, data: { source: 0 } })).toBe(false)
    expect(isUnserializedReplayEvent({ type: 2, timestamp: 1, data: { id: -1 } })).toBe(false)
    expect(isUnserializedReplayEvent(null)).toBe(false)
  })
})

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
  it('masks text inside private regions', () => {
    const maskText = createReplayMaskTextFn()
    const element = document.createElement('span')
    element.setAttribute('data-fidibeki-mask', '')
    expect(maskText('Jane Doe', element)).toBe('**** ***')
  })

  it('masks text with no associated element', () => {
    const maskText = createReplayMaskTextFn()
    expect(maskText('jane.doe@example.com', null)).toBe('********************')
  })

  it('preserves ordinary text inside a legacy safe-text region', () => {
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
    element.setAttribute('data-private', '')
    expect(maskText('Jane\nDoe', element)).toBe('****\n***')
  })
})

describe('sanitizeReplayEvent', () => {
  const sessionPageURL = 'https://example.com/accounts'
  const sanitizeReplayEvent = (event: unknown) =>
    sanitizeReplayEventForSession(event, sessionPageURL)

  it('replaces the internal Meta event href with the safe session location', () => {
    const event = { type: 4, timestamp: 1, data: { href: 'https://user:pass@example.com/dashboard?token=secret#frag' } }
    const sanitized = sanitizeReplayEvent(event) as typeof event
    expect(sanitized?.data.href).toBe(sessionPageURL)
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
                { type: 3, textContent: 'Ordinary page instructions' },
              ],
            },
          ],
        },
      },
    }
    const sanitized = sanitizeReplayEvent(event) as {
      data: { node: { childNodes: Array<{
        attributes: Record<string, unknown>
        childNodes: Array<{ textContent: string }>
      }> } }
    }
    const element = sanitized.data.node.childNodes[0]!
    expect(element.attributes).toEqual({ id: 'user-avatar-42', class: 'avatar', src: null, alt: '**** ***', title: '**** ***', 'aria-label': '**** ***', width: '80' })
    expect(element.childNodes[0]!.textContent).toBe(
      'Ordinary page instructions',
    )
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

  it('blanks CDATA text, which rrweb text-masking does not cover', () => {
    const event = {
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 0,
          childNodes: [{ type: 4, textContent: 'reporter: jane.doe@example.com' }],
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
    expect(sanitized.data.attributes[0]!.attributes).toEqual({ id: 'user-42', title: '**** ***', style: 'color: red;' })
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

  it('preserves sanitized native attributes when the mirror is unavailable', () => {
    const event = {
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 2, id: 31, tagName: 'div',
          attributes: { [SAFE_TEXT_ATTRIBUTE]: '', class: 'private-account-jane' },
          childNodes: [{ type: 3, textContent: 'Known static text' }],
        },
      },
    }

    expect(sanitizeReplayEventForSession(event, sessionPageURL, () => {
      throw new Error('mirror unavailable')
    })).toEqual({
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 2, id: 31, tagName: 'div',
          attributes: { [SAFE_TEXT_ATTRIBUTE]: '', class: 'private-account-jane' },
          childNodes: [{ type: 3, textContent: 'Known static text' }],
        },
      },
    })
  })

  it('passes through incremental sources unrelated to mutations unchanged', () => {
    const event = { type: 3, timestamp: 1, data: { source: 1, positions: [{ x: 1, y: 2 }] } }
    expect(sanitizeReplayEvent(event)).toEqual(event)
  })

  it('fails closed for unknown custom events', () => {
    const event = { type: 5, timestamp: 1, data: { tag: 'custom', payload: { foo: 'bar' } } }
    expect(sanitizeReplayEvent(event)).toBeNull()
  })

  it('pins the only supported navigation custom event to the safe session location', () => {
    const event = {
      type: 5, timestamp: 1,
      data: {
        tag: 'navigation',
        payload: { url: 'https://user:pass@example.com/account/jane?token=secret#private', extra: 'secret' },
      },
    }
    expect(sanitizeReplayEvent(event)).toEqual({
      type: 5, timestamp: 1,
      data: { tag: 'navigation', payload: { url: sessionPageURL } },
    })
  })

  it('fails closed when the safe session location is not canonical', () => {
    expect(sanitizeReplayEventForSession(
      { type: 0, timestamp: 1, data: {} },
      'https://user:pass@example.com/accounts?token=secret',
    )).toBeNull()
  })

  it('suppresses canvas, string-bearing, and unknown incremental sources', () => {
    for (const source of [9, 10, 11, 15, 999]) {
      expect(sanitizeReplayEvent({
        type: 3, timestamp: 1, data: { source, value: 'jane.doe@example.com' },
      })).toBeNull()
    }
  })

  it('masks mutation and input text independently of rrweb hooks', () => {
    const mutation = {
      type: 3, timestamp: 1,
      data: {
        source: 0, attributes: [], adds: [], removes: [],
        texts: [{ id: 7, value: 'jane@example.com' }],
      },
    }
    expect(sanitizeReplayEvent(mutation)).toEqual({
      ...mutation,
      data: { ...mutation.data, texts: [{ id: 7, value: '****************' }] },
    })
    const input = { type: 3, timestamp: 2, data: { source: 5, id: 8, text: 'jane@example.com' } }
    expect(sanitizeReplayEvent(input)).toEqual({
      type: 3, timestamp: 2, data: { source: 5, id: 8, text: '****************' },
    })
  })

  it('supports the legacy safe marker without discarding layout classes', () => {
    const event = {
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 2,
          attributes: { [SAFE_TEXT_ATTRIBUTE]: 'ignored', class: 'private-account' },
          childNodes: [{ type: 3, textContent: 'Known static text' }],
        },
      },
    }
    expect(sanitizeReplayEvent(event)).toEqual({
      type: 2, timestamp: 1,
      data: {
        node: {
          type: 2,
          attributes: { [SAFE_TEXT_ATTRIBUTE]: '', class: 'private-account' },
          childNodes: [{ type: 3, textContent: 'Known static text' }],
        },
      },
    })
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
