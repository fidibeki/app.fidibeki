import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  applyHostTheme,
  readHostDataTheme,
  resolveTheme,
  subscribeToHostDataTheme,
  subscribeToSystemTheme,
  systemTheme,
  type ResolveThemeOptions,
} from '../../src/theme-resolver'

function lightSystem(): void {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      media: '(prefers-color-scheme: dark)',
    }),
  )
}

function darkSystem(): void {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockReturnValue({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      media: '(prefers-color-scheme: dark)',
    }),
  )
}

function noMedia(): void {
  vi.stubGlobal('matchMedia', undefined)
}

function opts(overrides: Partial<ResolveThemeOptions> = {}): ResolveThemeOptions {
  return { fallback: 'auto', ...overrides }
}

describe('resolveTheme priority order', () => {
  beforeEach(() => {
    document.documentElement.removeAttribute('data-theme')
  })

  it('priority 1: element attribute wins over host data-theme and system', () => {
    darkSystem()
    document.documentElement.setAttribute('data-theme', 'light')
    expect(resolveTheme(opts({ elementAttr: 'dark', hostDataTheme: 'light' })))
      .toBe('dark')
  })

  it('priority 2: host data-theme wins over server fallback and system', () => {
    darkSystem()
    expect(resolveTheme(opts({ elementAttr: null, hostDataTheme: 'light', fallback: 'dark' })))
      .toBe('light')
  })

  it('priority 3a: explicit server fallback (light/dark) wins over system', () => {
    darkSystem()
    expect(resolveTheme(opts({ elementAttr: null, fallback: 'light' })))
      .toBe('light')
  })

  it('priority 3b: server "auto" delegates to the system theme', () => {
    darkSystem()
    expect(resolveTheme(opts({ elementAttr: null, fallback: 'auto' })))
      .toBe('dark')
  })

  it('system light is the final fallback when nothing else is set', () => {
    lightSystem()
    expect(resolveTheme(opts({}))).toBe('light')
  })

  it('ignores invalid element attribute values and falls through', () => {
    lightSystem()
    document.documentElement.setAttribute('data-theme', 'dark')
    expect(resolveTheme(opts({ elementAttr: 'blue', hostDataTheme: 'dark' })))
      .toBe('dark')
  })

  it('ignores invalid host data-theme values and falls through to fallback', () => {
    darkSystem()
    expect(resolveTheme(opts({ elementAttr: null, hostDataTheme: 'blue', fallback: 'light' })))
      .toBe('light')
  })

  it('defaults fallback to auto when omitted', () => {
    darkSystem()
    expect(resolveTheme({ elementAttr: null, hostDataTheme: null } as ResolveThemeOptions))
      .toBe('dark')
  })

  it('reads data-theme from documentElement when hostDataTheme is omitted', () => {
    lightSystem()
    document.documentElement.setAttribute('data-theme', 'dark')
    expect(resolveTheme({ elementAttr: null } as ResolveThemeOptions)).toBe('dark')
  })

  it('reads data-theme from closest ancestor when host provided', () => {
    lightSystem()
    const wrapper = document.createElement('div')
    wrapper.setAttribute('data-theme', 'dark')
    const inner = document.createElement('div')
    wrapper.append(inner)
    document.body.append(wrapper)
    expect(resolveTheme({ elementAttr: null, host: inner } as ResolveThemeOptions))
      .toBe('dark')
    wrapper.remove()
  })
})

describe('systemTheme', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('returns dark when prefers-color-scheme matches dark', () => {
    darkSystem()
    expect(systemTheme()).toBe('dark')
  })

  it('returns light when prefers-color-scheme does not match', () => {
    lightSystem()
    expect(systemTheme()).toBe('light')
  })

  it('defaults to light when matchMedia is unavailable', () => {
    noMedia()
    expect(systemTheme()).toBe('light')
  })
})

describe('readHostDataTheme', () => {
  beforeEach(() => {
    document.documentElement.removeAttribute('data-theme')
    document.body.innerHTML = ''
  })

  it('reads from documentElement by default', () => {
    document.documentElement.setAttribute('data-theme', 'dark')
    expect(readHostDataTheme()).toBe('dark')
  })

  it('reads from closest ancestor when host provided', () => {
    const wrapper = document.createElement('div')
    wrapper.setAttribute('data-theme', 'light')
    const inner = document.createElement('div')
    wrapper.append(inner)
    document.body.append(wrapper)
    expect(readHostDataTheme(inner)).toBe('light')
  })

  it('returns null when no ancestor carries data-theme', () => {
    expect(readHostDataTheme()).toBeNull()
  })
})

describe('applyHostTheme', () => {
  it('sets the theme attribute on the host element', () => {
    const host = document.createElement('div')
    applyHostTheme(host, 'dark')
    expect(host.getAttribute('theme')).toBe('dark')
  })

  it('overwrites a previous theme attribute', () => {
    const host = document.createElement('div')
    host.setAttribute('theme', 'dark')
    applyHostTheme(host, 'light')
    expect(host.getAttribute('theme')).toBe('light')
  })
})

describe('subscribeToSystemTheme', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('calls the listener when the media query fires a change', () => {
    const handlers: Array<(event: MediaQueryListEvent) => void> = []
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn((name: string, handler: (event: MediaQueryListEvent) => void) => {
        if (name === 'change') handlers.push(handler)
      }),
      removeEventListener: vi.fn(),
    }))
    const listener = vi.fn()
    const unsubscribe = subscribeToSystemTheme(listener)
    expect(handlers).toHaveLength(1)
    handlers[0]?.({ matches: true } as MediaQueryListEvent)
    expect(listener).toHaveBeenCalledTimes(1)
    unsubscribe()
  })

  it('returns a no-op unsubscribe when matchMedia is unavailable', () => {
    noMedia()
    const unsubscribe = subscribeToSystemTheme(() => {})
    expect(typeof unsubscribe).toBe('function')
    unsubscribe()
  })
})

describe('subscribeToHostDataTheme', () => {
  beforeEach(() => {
    document.documentElement.removeAttribute('data-theme')
  })

  it('fires when data-theme on documentElement changes', async () => {
    const listener = vi.fn()
    const unsubscribe = subscribeToHostDataTheme(listener)
    document.documentElement.setAttribute('data-theme', 'dark')
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(listener).toHaveBeenCalled()
    unsubscribe()
  })

  it('does not fire for unrelated attribute changes', async () => {
    const listener = vi.fn()
    const unsubscribe = subscribeToHostDataTheme(listener)
    document.documentElement.setAttribute('lang', 'pt')
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(listener).not.toHaveBeenCalled()
    unsubscribe()
  })

  it('stops observing after unsubscribe', async () => {
    const listener = vi.fn()
    const unsubscribe = subscribeToHostDataTheme(listener)
    unsubscribe()
    document.documentElement.setAttribute('data-theme', 'dark')
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(listener).not.toHaveBeenCalled()
  })
})