import { describe, expect, it } from 'vitest'
import { canForceDelete } from '~/utils/destructive-confirmation'

describe('canForceDelete', () => {
  it('requires the exact workspace slug after trimming only surrounding whitespace', () => {
    expect(canForceDelete('acme', 'acme')).toBe(true)
    expect(canForceDelete(' acme ', 'acme')).toBe(true)
    expect(canForceDelete('ACME', 'acme')).toBe(false)
    expect(canForceDelete('acme-2', 'acme')).toBe(false)
    expect(canForceDelete('', 'acme')).toBe(false)
  })
})
