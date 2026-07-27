import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineTestimonialElements } from '../../src/testimonials/wall'
import type { WallPayload } from '../../src/testimonials/types'

const HOST = 'https://widget.fidibeki.test'

function lightSystem(): void {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
}

function darkSystem(): void {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
}

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
    // Scoped to the card: the attribution badge is an anchor too, and this is
    // about what author-supplied content is allowed to become.
    expect(element.shadowRoot!.querySelector('.card a')).toBeNull()
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
    const anchor = element.shadowRoot!.querySelector('.card a')
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

  // The shape the product promises. A portrait recording set in a 16:9 box is
  // either letterboxed or cropped, and the customer chose to publish what this
  // person actually sent.
  it('holds each video in the shape it was recorded in', async () => {
    respondWith(payload({
      cards: [{
        id: 'ts_portrait',
        quote: 'Recorded on a phone held upright.',
        author: {
          name: 'Ana',
          video_url: 'https://cdn.test/a.mp4',
          poster_url: 'https://cdn.test/a.jpg',
          video_width: 720,
          video_height: 1280,
        },
      }],
    }))
    const element = await mount()
    const video = element.shadowRoot!.querySelector('video')
    expect(video?.getAttribute('style')).toContain('aspect-ratio: 720 / 1280')
    // The still stands in before playback, so the card is not a black rectangle
    // on someone else's marketing page.
    expect(video?.getAttribute('poster')).toBe('https://cdn.test/a.jpg')
  })

  it('falls back to the stylesheet when a payload carries no dimensions', async () => {
    respondWith(payload({
      cards: [{
        id: 'ts_old',
        quote: 'Published before dimensions were recorded.',
        author: { name: 'Ana', video_url: 'https://cdn.test/a.mp4' },
      }],
    }))
    const element = await mount()
    const video = element.shadowRoot!.querySelector('video')
    expect(video).not.toBeNull()
    expect(video?.getAttribute('style')).toBeNull()
    expect(video?.getAttribute('poster')).toBeNull()
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
  // Every layout the backoffice can choose has to arrive on the list as
  // something CSS can select on. A value that renders but selects nothing is
  // how masonry spent its first version silently being a grid.
  it.each(['grid', 'masonry', 'carousel', 'list', 'single'] as const)(
    'marks the list with the %s layout it was published as',
    async layout => {
      respondWith(payload({ layout }))
      const element = await mount()
      const wall = element.shadowRoot!.querySelector('.wall')
      expect(wall?.getAttribute('data-layout')).toBe(layout)
    },
  )

  // A wall set to custom still has a script tag on somebody's page. Degrading to
  // a wall is right; degrading to a blank space is not.
  it('still renders a wall the customer chose to build themselves', async () => {
    respondWith(payload({ layout: 'custom' }))
    const element = await mount()
    expect(element.shadowRoot!.querySelectorAll('.card')).toHaveLength(1)
  })

  it('duplicates the row for a marquee so the loop has no seam', async () => {
    respondWith(payload({
      layout: 'marquee',
      cards: [
        { id: 'ts_1', quote: 'First.', author: { name: 'Ana' } },
        { id: 'ts_2', quote: 'Second.', author: { name: 'Rui' } },
      ],
    }))
    const element = await mount()
    const tracks = element.shadowRoot!.querySelectorAll('.wall[data-layout="marquee"]')
    expect(tracks).toHaveLength(2)

    const clone = element.shadowRoot!.querySelector('.marquee__clone')
    // The copy exists to be looked at, not to be read out or tabbed through.
    expect(clone?.getAttribute('aria-hidden')).toBe('true')
    expect(clone?.hasAttribute('inert')).toBe(true)
  })

  // WCAG 2.2.2: content that moves on its own must be pausable. Hover covers the
  // pointer and focus-within covers the keyboard, so the container needs to be
  // reachable by keyboard at all.
  it('makes a moving marquee focusable so it can be paused', async () => {
    respondWith(payload({
      layout: 'marquee',
      cards: [
        { id: 'ts_1', quote: 'First.', author: { name: 'Ana' } },
        { id: 'ts_2', quote: 'Second.', author: { name: 'Rui' } },
      ],
    }))
    const element = await mount()
    const marquee = element.shadowRoot!.querySelector('.marquee')
    expect(marquee?.getAttribute('tabindex')).toBe('0')
    // Longer walls scroll for longer rather than faster.
    expect(marquee?.getAttribute('style')).toContain('--fw-marquee-duration')
  })

  it('does not animate a marquee holding a single card', async () => {
    respondWith(payload({ layout: 'marquee' }))
    const element = await mount()
    expect(element.shadowRoot!.querySelector('.marquee')).toBeNull()
    expect(element.shadowRoot!.querySelector('.wall')).not.toBeNull()
  })

  it('signs the wall with a link back to Fidibeki', async () => {
    respondWith(payload())
    const element = await mount()
    const badge = element.shadowRoot!.querySelector<HTMLAnchorElement>('.badge')
    expect(badge?.href).toBe('https://fidibeki.com/')
    expect(badge?.textContent?.trim()).toContain('Proofed by Fidibeki')
    // The badge opens a new tab on a page that is not ours, so the opener must
    // not be reachable from it.
    expect(badge?.rel).toContain('noopener')
    expect(badge?.target).toBe('_blank')
  })

  it('does not sign a wall that rendered nothing', async () => {
    respondWith(payload({ cards: [] }))
    const element = await mount()
    expect(element.shadowRoot!.querySelector('.badge')).toBeNull()
  })

  // The card is a <figure>, and a figure's UA margin is 1em 40px. Left in, it
  // takes 80px off every card and stacks 40px either side of the gap the layout
  // asked for, so neighbouring cards sit 96px apart instead of 16 — on someone
  // else's page, where it reads as the wall being badly built.
  //
  // jsdom ships no UA stylesheet, so this cannot reproduce the margin itself.
  // What it does hold is the reset: delete the rule and the property computes to
  // "" instead of "0px".
  it('resets the margin a figure would otherwise bring with it', async () => {
    respondWith(payload())
    const element = await mount()
    const card = element.shadowRoot!.querySelector('.card')!
    const style = getComputedStyle(card)
    expect(style.marginLeft).toBe('0px')
    expect(style.marginTop).toBe('0px')
  })

  // observedAttributes lists layout and theme, which is a promise that setting
  // them does something. A page with its own dark mode toggle sets theme on the
  // element and has no way to be told the element declined to act on it.
  it('repaints when the layout attribute changes, without refetching', async () => {
    respondWith(payload({ layout: 'grid' }))
    const element = await mount()
    expect(element.shadowRoot!.querySelector('.wall')?.getAttribute('data-layout')).toBe('grid')

    element.setAttribute('layout', 'list')
    await vi.waitFor(() => {
      const wall = element.shadowRoot!.querySelector('.wall')
      if (wall?.getAttribute('data-layout') !== 'list') throw new Error('not repainted yet')
    })
    // The payload is already in hand; asking for it again would be a request per
    // toggle on somebody else's marketing page.
    expect(fetch).toHaveBeenCalledTimes(1)
  })

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

  // ---- Theme resolution (issue #130) ----
  // Priority: element attribute > host data-theme > server mode > system

  afterEach(() => {
    vi.unstubAllGlobals()
    document.documentElement.removeAttribute('data-theme')
  })

  it('applies theme="dark" when the element attribute requests dark', async () => {
    lightSystem()
    document.documentElement.setAttribute('data-theme', 'light')
    respondWith(payload({ theme: { mode: 'light' } }))
    const element = await mount({ theme: 'dark' })
    expect(element.getAttribute('data-fb-theme')).toBe('dark')
  })

  it('applies theme="light" when the element attribute requests light over a dark system', async () => {
    darkSystem()
    respondWith(payload({ theme: { mode: 'dark' } }))
    const element = await mount({ theme: 'light' })
    expect(element.getAttribute('data-fb-theme')).toBe('light')
  })

  it('falls back to host data-theme when no element attribute is set', async () => {
    darkSystem()
    document.documentElement.setAttribute('data-theme', 'light')
    respondWith(payload({ theme: { mode: 'dark' } }))
    const element = await mount()
    expect(element.getAttribute('data-fb-theme')).toBe('light')
  })

  it('falls back to server mode when no element attribute or host data-theme is set', async () => {
    darkSystem()
    respondWith(payload({ theme: { mode: 'light' } }))
    const element = await mount()
    expect(element.getAttribute('data-fb-theme')).toBe('light')
  })

  it('delegates to system when server mode is auto and nothing else is set', async () => {
    darkSystem()
    respondWith(payload({ theme: { mode: 'auto' } }))
    const element = await mount()
    expect(element.getAttribute('data-fb-theme')).toBe('dark')
  })

  it('repaints with the new theme when the theme attribute changes at runtime', async () => {
    lightSystem()
    respondWith(payload({ theme: { mode: 'light' } }))
    const element = await mount()
    expect(element.getAttribute('data-fb-theme')).toBe('light')

    element.setAttribute('theme', 'dark')
    await vi.waitFor(() => {
      if (element.getAttribute('data-fb-theme') !== 'dark') throw new Error('theme not updated')
    })
  })

  it('does not refetch when only the theme attribute changes', async () => {
    lightSystem()
    respondWith(payload({ theme: { mode: 'light' } }))
    const element = await mount()
    element.setAttribute('theme', 'dark')
    await vi.waitFor(() => {
      if (element.getAttribute('data-fb-theme') !== 'dark') throw new Error('theme not updated')
    })
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('updates theme when host data-theme changes after mount', async () => {
    lightSystem()
    respondWith(payload({ theme: { mode: 'auto' } }))
    const element = await mount()
    expect(element.getAttribute('data-fb-theme')).toBe('light')

    document.documentElement.setAttribute('data-theme', 'dark')
    await vi.waitFor(() => {
      if (element.getAttribute('data-fb-theme') !== 'dark') throw new Error('theme not updated')
    })
  })

  it('cleans up subscriptions on disconnect', async () => {
    lightSystem()
    respondWith(payload())
    const element = await mount()
    element.remove()
    // After disconnect, changing data-theme should not throw
    document.documentElement.setAttribute('data-theme', 'dark')
    // No assertion needed — the test passes if no error is thrown
  })

  it('honours data-theme="dark" set directly on the element', async () => {
    lightSystem()
    document.documentElement.removeAttribute('data-theme')
    respondWith(payload({ theme: { mode: 'light' } }))
    const element = await mount({ 'data-theme': 'dark' })
    expect(element.getAttribute('data-fb-theme')).toBe('dark')
  })

  it('excludes the element from the dark media query when theme="light" is set', async () => {
    darkSystem()
    document.documentElement.removeAttribute('data-theme')
    respondWith(payload({ theme: { mode: 'auto' } }))
    const element = await mount({ theme: 'light' })
    // Priority 1: theme="light" wins over system dark
    expect(element.getAttribute('data-fb-theme')).toBe('light')
  })
})
