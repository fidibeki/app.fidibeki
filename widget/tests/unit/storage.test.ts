import { MemoryStorage, storageOrMemory } from '../../src/storage'

describe('widget storage', () => {
  it('implements the browser storage contract in memory', () => {
    const storage = new MemoryStorage()
    storage.setItem('first', 'one')
    storage.setItem('second', 'two')

    expect(storage.length).toBe(2)
    expect(storage.key(0)).toBe('first')
    expect(storage.getItem('second')).toBe('two')
    storage.removeItem('first')
    expect(storage.length).toBe(1)
    storage.clear()
    expect(storage.length).toBe(0)
  })

  it('uses browser storage only after a successful write probe', () => {
    const storage = new MemoryStorage()
    expect(storageOrMemory(() => storage as Storage)).toBe(storage)
    expect([...Array(storage.length)]).toHaveLength(0)
  })

  it('falls back to page-lifetime memory when storage access or writes are blocked', () => {
    const inaccessible = storageOrMemory(() => { throw new Error('blocked') })
    inaccessible.setItem('state', 'submitted')
    expect(inaccessible.getItem('state')).toBe('submitted')

    const unwritable = storageOrMemory(() => ({
      setItem: () => { throw new Error('quota') },
    } as unknown as Storage))
    unwritable.setItem('state', 'dismissed')
    expect(unwritable.getItem('state')).toBe('dismissed')
  })
})
