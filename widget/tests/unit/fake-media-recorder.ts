export class FakeMediaStreamTrack {
  stopped = false
  stop(): void {
    this.stopped = true
  }
}

export class FakeMediaStream {
  private readonly tracks: FakeMediaStreamTrack[]

  constructor(trackCount = 1) {
    this.tracks = Array.from({ length: trackCount }, () => new FakeMediaStreamTrack())
  }

  getTracks(): FakeMediaStreamTrack[] {
    return this.tracks
  }
}

interface FakeListener {
  handler: (event?: unknown) => void
  once: boolean
}

// A minimal MediaRecorder stand-in that mirrors the two properties createAudioControl/
// createVideoControl actually depend on: an explicit constructor mimeType option, and the
// `mimeType` the "browser" ends up reporting once recording starts (which real UAs sometimes
// normalize away codec parameters on, so tests can exercise that divergence too).
export class FakeMediaRecorder {
  static supportedTypes = new Set<string>()
  static instances: FakeMediaRecorder[] = []
  static reportedMimeType: string | undefined

  static isTypeSupported(type: string): boolean {
    return FakeMediaRecorder.supportedTypes.has(type)
  }

  static reset(supportedTypes: string[] = [], reportedMimeType?: string): void {
    FakeMediaRecorder.supportedTypes = new Set(supportedTypes)
    FakeMediaRecorder.instances = []
    FakeMediaRecorder.reportedMimeType = reportedMimeType
  }

  readonly requestedMimeType: string | undefined
  mimeType: string
  state: 'inactive' | 'recording' = 'inactive'
  private readonly listeners = new Map<string, FakeListener[]>()

  constructor(_stream: unknown, options?: { mimeType?: string }) {
    this.requestedMimeType = options?.mimeType
    this.mimeType = FakeMediaRecorder.reportedMimeType ?? options?.mimeType ?? ''
    FakeMediaRecorder.instances.push(this)
  }

  addEventListener(type: string, handler: (event?: unknown) => void, options?: { once?: boolean }): void {
    const list = this.listeners.get(type) ?? []
    list.push({ handler, once: options?.once ?? false })
    this.listeners.set(type, list)
  }

  start(): void {
    this.state = 'recording'
  }

  stop(): void {
    this.state = 'inactive'
    this.emit('dataavailable', { data: new Blob(['fixture-chunk'], { type: this.mimeType }) })
    this.emit('stop')
  }

  private emit(type: string, event?: unknown): void {
    const list = this.listeners.get(type) ?? []
    for (const entry of [...list]) entry.handler(event)
    this.listeners.set(type, list.filter(entry => !entry.once))
  }
}

export function installFakeMediaRecordingEnvironment(): { restore(): void } {
  const originalMediaRecorder = (globalThis as { MediaRecorder?: unknown }).MediaRecorder
  const originalMediaDevices = navigator.mediaDevices;
  (globalThis as { MediaRecorder?: unknown }).MediaRecorder = FakeMediaRecorder
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: async () => new FakeMediaStream() },
  })
  return {
    restore(): void {
      (globalThis as { MediaRecorder?: unknown }).MediaRecorder = originalMediaRecorder
      Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: originalMediaDevices })
    },
  }
}
