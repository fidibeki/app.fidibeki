import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import {
  MobileClientError,
  normalizeScreen,
  recurrenceStorageKey,
  validateCaptureSession,
} from '../src'

function fixture<T>(name: string): T {
  const url = new URL(`../../contracts/mobile-capture/fixtures/${name}`, import.meta.url)
  return JSON.parse(readFileSync(url, 'utf8')) as T
}

describe('shared mobile capture contracts', () => {
  it('accepts the versioned valid session fixture', () => {
    const parsed = validateCaptureSession(fixture('session-valid.json'))
    expect(parsed.configuration.schema_version).toBe(1)
    expect(parsed.configuration.definition.response_blocks).toHaveLength(4)
  })

  it('rejects an unsupported session schema', () => {
    expect(() => validateCaptureSession(fixture('session-invalid-schema.json'))).toThrowError(
      MobileClientError,
    )
  })

  it('normalizes shared screen cases and uses the normalized recurrence component', () => {
    const contract = fixture<{
      cases: Array<{ input: string; expected: string; encoded_component: string }>
    }>('screen-normalization.json')
    for (const testCase of contract.cases) {
      expect(normalizeScreen(testCase.input)).toBe(testCase.expected)
      expect(recurrenceStorageKey('project', 'definition', testCase.input)).toContain(
        testCase.encoded_component,
      )
    }
  })
})
