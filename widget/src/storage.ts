export interface StorageLike {
  getItem(key: string): string | null
  removeItem(key: string): void
  setItem(key: string, value: string): void
}

export interface EnumerableStorageLike extends StorageLike {
  readonly length: number
  clear(): void
  key(index: number): string | null
}

export class MemoryStorage implements EnumerableStorageLike {
  private readonly values = new Map<string, string>()

  get length(): number { return this.values.size }

  clear(): void { this.values.clear() }

  getItem(key: string): string | null { return this.values.get(key) ?? null }

  key(index: number): string | null { return [...this.values.keys()][index] ?? null }

  removeItem(key: string): void { this.values.delete(key) }

  setItem(key: string, value: string): void { this.values.set(key, String(value)) }
}

export function storageOrMemory(getStorage: () => Storage): EnumerableStorageLike {
  try {
    const storage = getStorage()
    const probeKey = `fidibeki:storage-probe:${randomSuffix()}`
    storage.setItem(probeKey, probeKey)
    if (storage.getItem(probeKey) !== probeKey) throw new Error('Storage probe failed')
    storage.removeItem(probeKey)
    return storage
  } catch {
    return new MemoryStorage()
  }
}

function randomSuffix(): string {
  return globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2)
}
