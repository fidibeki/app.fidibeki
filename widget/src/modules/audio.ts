export function createAudioControl(): { element: HTMLElement; recording(): Promise<Blob | null>; destroy(): void } {
  const button = document.createElement('button')
  button.type = 'button'
  button.className = 'secondary'
  button.textContent = 'Record audio'
  let recorder: MediaRecorder | null = null
  let stream: MediaStream | null = null
  let result: Blob | null = null
  let chunks: BlobPart[] = []
	let stopped: Promise<void> = Promise.resolve()

  button.addEventListener('click', async () => {
    if (recorder?.state === 'recording') {
      recorder.stop()
      button.textContent = 'Audio attached'
      return
    }
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      chunks = []
      recorder = new MediaRecorder(stream)
			stopped = new Promise((resolve) => recorder?.addEventListener('stop', () => resolve(), { once: true }))
      recorder.addEventListener('dataavailable', (event) => {
        if (event.data.size > 0) chunks.push(event.data)
      })
      recorder.addEventListener('stop', () => {
        result = new Blob(chunks, { type: recorder?.mimeType || 'audio/webm' })
        stream?.getTracks().forEach((track) => track.stop())
      })
      recorder.start()
      button.textContent = 'Stop recording'
    } catch {
      button.textContent = 'Microphone unavailable'
      button.disabled = true
    }
  })

  return {
    element: button,
		recording: async () => {
			if (recorder?.state === 'recording') recorder.stop()
			await stopped
			return result
		},
    destroy: () => {
      if (recorder?.state === 'recording') recorder.stop()
      stream?.getTracks().forEach((track) => track.stop())
    },
  }
}
