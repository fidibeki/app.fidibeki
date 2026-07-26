import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineTestimonialElements } from '../../src/testimonials/wall'
import type { WallPayload } from '../../src/testimonials/types'

const HOST = 'https://widget.fidibeki.test'

function payload(overrides: Partial<WallPayload> = {}): WallPayload {
  return {
    schema_version: 1,
    wall: 'wl_0123456789abcdef',
    name: 'Homepage',
    layout: 'grid',
    theme: {},
    show_rating: true,
    show_avatar: true,
    show_company: true,
    show_video: true,
    generated_at: '2026-07-26T12:00:00Z',
    cards: [
      {
        id: 'ts_0123456789abcdef',
        quote: 'We cut triage from 3 days to 20 minutes.',
        rating: 5,
        author: { name: 'Ana Marques', role: 'Head of Support', company: 'Acme' },
      },
    ],
    ...overrides,
  }
}

function respondWith(body: unknown, ok = true) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok,
    json: async () => body,
  }))
}

/** Mounts the element and waits for its fetch to settle. */
async function mount(attributes: Record<string, string> = {}) {
  const element = document.createElement('fidibeki-wall')
  element.setAttribute('wall', 'wl_0123456789abcdef')
  element.setAttribute('host', HOST)
  for (const [name, value] of Object.entries(attributes)) element.setAttribute(name, value)
  document.body.append(element)
  await vi.waitFor(() => {
    if (!element.shadowRoot) throw new Error('no shadow root yet')
  })
  await new Promise(resolve => setTimeout(resolve, 0))
  return element
}

describe('fidibeki-wall', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    defineTestimonialElements(HOST)
  })

  it('renders a consented card as semantic markup inside a shadow root', async () => {
    respondWith(payload())
    const element = await mount()
    const root = element.shadowRoot!
    expect(root.querySelector('blockquote')?.textContent).toBe(
      'We cut triage from 3 days to 20 minutes.',
    )
    expect(root.querySelector('cite')?.textContent).toBe('Ana Marques')
    // Shadow DOM is what stops the host page's CSS reaching in, or ours leaking out.
    expect(element.innerHTML).toBe('')
  })

  // A testimonial is author-supplied content that we publish on someone else's
  // marketing site. Escaping is not a nicety here.
  it('escapes markup in author-supplied text', async () => {
    respondWith(payload({
      cards: [{
        id: 'ts_x',
        quote: '<img src=x onerror="alert(1)">',
        author: { name: '<script>alert(2)</script>' },
      }],
    }))
    const element = await mount()
    const root = element.shadowRoot!
    expect(root.querySelector('img')).toBeNull()
    expect(root.querySelector('script')).toBeNull()
    expect(root.querySelector('blockquote')?.textContent).toBe('<img src=x onerror="alert(1)">')
  })

  it('refuses a javascript: company URL', async () => {
    respondWith(payload({
      cards: [{
        id: 'ts_x',
        quote: 'Good product.',
        author: { name: 'Ana', company: 'Acme', company_url: 'javascript:alert(1)' },
      }],
    }))
    const element = await mount()
    const anchor = element.shadowRoot!.querySelector('a')
    expect(anchor).toBeNull()
    // The company name still renders; only the dangerous link is dropped.
    expect(element.shadowRoot!.textContent).toContain('Acme')
  })

  it('renders an https company URL with rel protections', async () => {
    respondWith(payload({
      cards: [{
        id: 'ts_x',
        quote: 'Good product.',
        author: { name: 'Ana', company: 'Acme', company_url: 'https://acme.test' },
      }],
    }))
    const element = await mount()
    const anchor = element.shadowRoot!.querySelector('a')
    expect(anchor?.getAttribute('href')).toBe('https://acme.test/')
    expect(anchor?.getAttribute('rel')).toContain('noopener')
  })

  // Wall-level toggles are the customer's choice and must be obeyed by the
  // renderer, not just by the server that built the payload.
  it('honours the wall display toggles', async () => {
    respondWith(payload({
      show_rating: false,
      show_avatar: false,
      cards: [{
        id: 'ts_x',
        quote: 'Good product.',
        rating: 5,
        author: { name: 'Ana', avatar_url: 'https://cdn.test/a.jpg' },
      }],
    }))
    const element = await mount()
    expect(element.shadowRoot!.querySelector('.rating')).toBeNull()
    expect(element.shadowRoot!.querySelector('img.avatar')).toBeNull()
  })

  it('does not autoplay video and does not preload it', async () => {
    respondWith(payload({
      cards: [{
        id: 'ts_x',
        quote: 'Good product.',
        author: { name: 'Ana', video_url: 'https://cdn.test/a.mp4' },
      }],
    }))
    const element = await mount()
    const video = element.shadowRoot!.querySelector('video')
    expect(video).not.toBeNull()
    expect(video?.hasAttribute('autoplay')).toBe(false)
    expect(video?.getAttribute('preload')).toBe('none')
  })

  // A stale embed on a customer's site must not render a payload it cannot
  // understand: a half-broken wall is worse than no wall.
  it('renders nothing for a newer schema version', async () => {
    respondWith(payload({ schema_version: 99 }))
    const element = await mount()
    expect(element.shadowRoot!.innerHTML).toBe('')
  })

  it('renders nothing when the request fails', async () => {
    respondWith({}, false)
    const element = await mount()
    expect(element.shadowRoot!.innerHTML).toBe('')
  })

  it('renders nothing when the wall has no cards', async () => {
    respondWith(payload({ cards: [] }))
    const element = await mount()
    expect(element.shadowRoot!.innerHTML).toBe('')
  })

  it('renders nothing when the response is not a wall payload', async () => {
    respondWith({ hello: 'world' })
    const element = await mount()
    expect(element.shadowRoot!.innerHTML).toBe('')
  })

  it('requests the wall from the public endpoint', async () => {
    respondWith(payload())
    await mount()
    expect(fetch).toHaveBeenCalledWith(
      `${HOST}/v1/public/walls/wl_0123456789abcdef`,
      expect.objectContaining({ credentials: 'omit' }),
    )
  })

  it('registers the single-testimonial element as well', () => {
    expect(customElements.get('fidibeki-testimonial')).toBeDefined()
  })
})
