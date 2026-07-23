import { describe, expect, it } from 'vitest'
import { parseSignedDownloadURL } from '../../utils/download'

describe('parseSignedDownloadURL', () => {
  it('accepts absolute and same-origin relative HTTP URLs', () => {
    expect(parseSignedDownloadURL(
      'https://downloads.example.test/export.zip?signature=private',
      'https://acme.fidibeki.com',
    )).toBe('https://downloads.example.test/export.zip?signature=private')
    expect(parseSignedDownloadURL('/v1/download/export.zip', 'http://localhost:3000'))
      .toBe('http://localhost:3000/v1/download/export.zip')
  })

  it.each([
    'javascript:alert(1)',
    'data:application/zip;base64,UEsDBA==',
    'https://user:secret@downloads.example.test/export.zip',
  ])('rejects an unsafe signed URL: %s', (value) => {
    expect(() => parseSignedDownloadURL(value, 'https://acme.fidibeki.com'))
      .toThrow('signed download URL is invalid')
  })
})
