import { isSuppressed, pageIdentity, suppress, suppressionKey, type SessionStore } from '../../src/suppression'

class MemoryStore implements SessionStore {
  values = new Map<string, string>()
  getItem(key: string): string | null { return this.values.get(key) ?? null }
  setItem(key: string, value: string): void { this.values.set(key, value) }
}

describe('session suppression', () => {
  it('uses path and query but ignores the hash', () => {
    expect(pageIdentity('https://example.com/docs?page=2#section')).toBe('/docs?page=2')
    expect(suppressionKey('project', 'rule', 'https://example.com/docs')).toContain('project:rule:/docs')
  })

  it('suppresses only the same project, rule, and page', () => {
    const store = new MemoryStore()
    suppress(store, 'project', 'rule', 'https://example.com/docs')
    expect(isSuppressed(store, 'project', 'rule', 'https://example.com/docs#next')).toBe(true)
    expect(isSuppressed(store, 'project', 'rule', 'https://example.com/other')).toBe(false)
    expect(isSuppressed(store, 'project', 'another', 'https://example.com/docs')).toBe(false)
  })
})
