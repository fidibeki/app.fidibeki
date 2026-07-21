export function createScreenshotControl(): { element: HTMLElement; screenshot(): Blob | null } {
	const button = document.createElement('button')
	button.type = 'button'
	button.className = 'secondary'
	button.textContent = 'Capture screenshot'
	let captured: Blob | null = null

	button.addEventListener('click', async () => {
		try {
			const stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
			const video = document.createElement('video')
			video.srcObject = stream
			video.muted = true
			await video.play()
			const canvas = document.createElement('canvas')
			canvas.width = video.videoWidth
			canvas.height = video.videoHeight
			canvas.getContext('2d')?.drawImage(video, 0, 0)
			captured = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
			stream.getTracks().forEach((track) => track.stop())
			video.srcObject = null
			button.textContent = captured ? 'Screenshot attached' : 'Capture screenshot'
		} catch {
			button.textContent = 'Screenshot unavailable'
		}
	})

	return { element: button, screenshot: () => captured }
}
