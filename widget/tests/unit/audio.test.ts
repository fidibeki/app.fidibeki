import { createAudioControl } from '../../src/modules/audio'
import { FakeMediaRecorder, installFakeMediaRecordingEnvironment } from './fake-media-recorder'

async function flush(): Promise<void> {
  await new Promise(resolve => setTimeout(resolve, 0))
}

describe('createAudioControl', () => {
  let environment: { restore(): void }

  beforeEach(() => {
    environment = installFakeMediaRecordingEnvironment()
  })

  afterEach(() => {
    environment.restore()
  })

  it('requests an explicit, deterministic MIME type instead of the browser implicit default', async () => {
    FakeMediaRecorder.reset(['audio/webm;codecs=opus', 'audio/webm'])
    const control = createAudioControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(FakeMediaRecorder.instances[0]?.requestedMimeType).toBe('audio/webm;codecs=opus')
    expect(blob?.type).toBe('audio/webm;codecs=opus')
  })

  it('declares Firefox-style Ogg output as audio/ogg, never falling back to a webm literal', async () => {
    // Firefox does not support the qualified webm/opus candidates in this scenario, so the
    // negotiated (and actually produced) container is Ogg.
    FakeMediaRecorder.reset(['audio/ogg;codecs=opus', 'audio/ogg'])
    const control = createAudioControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(blob?.type).toBe('audio/ogg;codecs=opus')
    expect(blob?.type).not.toContain('webm')
  })

  it('trusts the actual recorder mimeType over the requested candidate when a UA normalizes it', async () => {
    // Some UAs report a slightly different mimeType than requested (e.g. dropping codec
    // parameters). The declared type must reflect what was actually produced.
    FakeMediaRecorder.reset(['audio/webm;codecs=opus'], 'audio/webm')
    const control = createAudioControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(blob?.type).toBe('audio/webm')
  })

  it('only falls back to the generic webm literal when no MIME signal is available at all', async () => {
    FakeMediaRecorder.reset([])
    const control = createAudioControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(FakeMediaRecorder.instances[0]?.requestedMimeType).toBeUndefined()
    expect(blob?.type).toBe('audio/webm')
  })

  it('returns null and does not attach anything when no recording was made', async () => {
    FakeMediaRecorder.reset(['audio/webm;codecs=opus'])
    const control = createAudioControl()
    await expect(control.recording()).resolves.toBeNull()
  })
})
