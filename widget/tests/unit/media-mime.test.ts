import {
  AUDIO_MIME_CANDIDATES,
  VIDEO_MIME_CANDIDATES,
  actualChunkMimeType,
  resolveRecorderMimeType,
} from '../../src/modules/media-mime'

describe('resolveRecorderMimeType', () => {
  it('picks the first candidate the browser reports as supported', () => {
    const supported = new Set(['audio/ogg;codecs=opus', 'audio/ogg'])
    expect(resolveRecorderMimeType(AUDIO_MIME_CANDIDATES, type => supported.has(type)))
      .toBe('audio/ogg;codecs=opus')
  })

  it('is deterministic: Chrome-like support picks webm, Firefox-like support picks ogg', () => {
    const chromeLike = new Set(['audio/webm;codecs=opus', 'audio/webm'])
    const firefoxLike = new Set(['audio/ogg;codecs=opus', 'audio/ogg'])
    expect(resolveRecorderMimeType(AUDIO_MIME_CANDIDATES, type => chromeLike.has(type)))
      .toBe('audio/webm;codecs=opus')
    expect(resolveRecorderMimeType(AUDIO_MIME_CANDIDATES, type => firefoxLike.has(type)))
      .toBe('audio/ogg;codecs=opus')
  })

  it('resolves a deterministic video candidate across the codec-qualified webm variants', () => {
    const supported = new Set(['video/webm;codecs=vp8,opus', 'video/webm'])
    expect(resolveRecorderMimeType(VIDEO_MIME_CANDIDATES, type => supported.has(type)))
      .toBe('video/webm;codecs=vp8,opus')
  })

  it('returns undefined when nothing in the candidate list is supported', () => {
    expect(resolveRecorderMimeType(AUDIO_MIME_CANDIDATES, () => false)).toBeUndefined()
  })

  it('treats a throwing isTypeSupported implementation as unsupported and keeps checking', () => {
    const resolved = resolveRecorderMimeType(AUDIO_MIME_CANDIDATES, (type) => {
      if (type === 'audio/webm;codecs=opus') throw new Error('boom')
      return type === 'audio/ogg;codecs=opus'
    })
    expect(resolved).toBe('audio/ogg;codecs=opus')
  })
})

describe('actualChunkMimeType', () => {
  it('always prefers what the recorder actually reports over the requested candidate', () => {
    expect(actualChunkMimeType({ mimeType: 'audio/ogg;codecs=opus' }, 'audio/webm;codecs=opus'))
      .toBe('audio/ogg;codecs=opus')
  })

  it('falls back to the requested candidate when the recorder reports nothing', () => {
    expect(actualChunkMimeType({ mimeType: '' }, 'audio/ogg;codecs=opus')).toBe('audio/ogg;codecs=opus')
    expect(actualChunkMimeType(null, 'audio/ogg;codecs=opus')).toBe('audio/ogg;codecs=opus')
  })

  it('never invents a mime type when neither the recorder nor a requested candidate is known', () => {
    expect(actualChunkMimeType(null, undefined)).toBeUndefined()
  })
})
