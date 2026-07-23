import { createVideoControl } from '../../src/modules/video'
import { FakeMediaRecorder, installFakeMediaRecordingEnvironment } from './fake-media-recorder'

async function flush(): Promise<void> {
  await new Promise(resolve => setTimeout(resolve, 0))
}

describe('createVideoControl', () => {
  let environment: { restore(): void }

  beforeEach(() => {
    environment = installFakeMediaRecordingEnvironment()
  })

  afterEach(() => {
    environment.restore()
  })

  it('requests an explicit, deterministic MIME type instead of the browser implicit default', async () => {
    FakeMediaRecorder.reset(['video/webm;codecs=vp9,opus', 'video/webm'])
    const control = createVideoControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(FakeMediaRecorder.instances[0]?.requestedMimeType).toBe('video/webm;codecs=vp9,opus')
    expect(blob?.type).toBe('video/webm;codecs=vp9,opus')
  })

  it('falls back through the ordered candidate list when the preferred codec is unsupported', async () => {
    FakeMediaRecorder.reset(['video/webm;codecs=vp8,opus', 'video/webm', 'video/mp4'])
    const control = createVideoControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(FakeMediaRecorder.instances[0]?.requestedMimeType).toBe('video/webm;codecs=vp8,opus')
    expect(blob?.type).toBe('video/webm;codecs=vp8,opus')
  })

  it('only falls back to the generic webm literal when no MIME signal is available at all', async () => {
    FakeMediaRecorder.reset([])
    const control = createVideoControl()

    control.element.dispatchEvent(new Event('click'))
    await flush()
    control.element.dispatchEvent(new Event('click'))

    const blob = await control.recording()
    expect(FakeMediaRecorder.instances[0]?.requestedMimeType).toBeUndefined()
    expect(blob?.type).toBe('video/webm')
  })

  it('returns null and does not attach anything when no recording was made', async () => {
    FakeMediaRecorder.reset(['video/webm;codecs=vp9,opus'])
    const control = createVideoControl()
    await expect(control.recording()).resolves.toBeNull()
  })
})
