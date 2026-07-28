import { describe, expect, it } from 'vitest'
import { File } from 'node:buffer'
import { buildPresignedPostBody } from '~/utils/presigned-upload'

describe('presigned POST upload body', () => {
  it('keeps every signed field and appends the file last', () => {
    const file = new File(['video'], 'clip.webm', { type: 'video/webm' })
    const body = buildPresignedPostBody({
      key: 'testimonials/tenant/media/source',
      policy: 'signed-policy',
      'Content-Type': 'video/webm',
    }, file)

    expect(Array.from(body.keys())).toEqual(['key', 'policy', 'Content-Type', 'file'])
    const stored = body.get('file') as File
    expect({ name: stored.name, size: stored.size, type: stored.type }).toEqual({
      name: 'clip.webm', size: 5, type: 'video/webm',
    })
  })
})
