type CaptureScreenshot = () => Promise<Blob | null>

interface Point {
	x: number
	y: number
}

interface Stroke {
	color: string
	width: number
	points: Point[]
}

export function createScreenshotControl(capture?: CaptureScreenshot): { element: HTMLElement; screenshot(): Blob | null; destroy(): void } {
	const root = document.createElement('div')
	root.className = 'fb-screenshot'
	root.innerHTML = `${controlStyles()}
		<button class="fb-screenshot__capture" type="button">Capture screenshot</button>
		<div class="fb-screenshot__summary" hidden>
			<img alt="Screenshot preview">
			<span><strong>Screenshot attached</strong><small>PNG with your annotations</small></span>
			<div><button type="button" data-edit>Edit screenshot</button><button type="button" data-remove>Remove screenshot</button></div>
		</div>
		<dialog class="fb-screenshot__dialog" aria-labelledby="fb-screenshot-title">
			<section>
				<header><div><span>SCREENSHOT</span><h3 id="fb-screenshot-title">Annotate screenshot</h3></div><button type="button" data-close aria-label="Close editor" title="Close editor">&times;</button></header>
				<div class="fb-screenshot__stage"><canvas aria-label="Screenshot annotation canvas"></canvas></div>
				<div class="fb-screenshot__toolbar">
					<fieldset><legend>Color</legend><div class="fb-screenshot__colors">
						<button type="button" data-color="#d92d20" aria-label="Red" title="Red" aria-pressed="true"></button>
						<button type="button" data-color="#f5b700" aria-label="Yellow" title="Yellow" aria-pressed="false"></button>
						<button type="button" data-color="#2563eb" aria-label="Blue" title="Blue" aria-pressed="false"></button>
					</div></fieldset>
					<label>Brush size<input type="range" min="2" max="18" value="6" aria-label="Brush size"></label>
					<div class="fb-screenshot__history"><button type="button" data-undo disabled title="Undo last mark">Undo</button><button type="button" data-clear disabled title="Clear all marks">Clear marks</button></div>
				</div>
				<footer><button type="button" data-cancel>Cancel</button><button type="button" data-save>Use screenshot</button></footer>
			</section>
		</dialog>`

	const captureButton = required<HTMLButtonElement>(root, '.fb-screenshot__capture')
	const summary = required<HTMLElement>(root, '.fb-screenshot__summary')
	const preview = required<HTMLImageElement>(root, 'img')
	const editButton = required<HTMLButtonElement>(root, '[data-edit]')
	const removeButton = required<HTMLButtonElement>(root, '[data-remove]')
	const dialog = required<HTMLDialogElement>(root, 'dialog')
	const canvas = required<HTMLCanvasElement>(root, 'canvas')
	const closeButton = required<HTMLButtonElement>(root, '[data-close]')
	const cancelButton = required<HTMLButtonElement>(root, '[data-cancel]')
	const saveButton = required<HTMLButtonElement>(root, '[data-save]')
	const undoButton = required<HTMLButtonElement>(root, '[data-undo]')
	const clearButton = required<HTMLButtonElement>(root, '[data-clear]')
	const brushSize = required<HTMLInputElement>(root, 'input[type="range"]')
	const colorButtons = Array.from(root.querySelectorAll<HTMLButtonElement>('[data-color]'))
	const context = canvas.getContext('2d')
	const source = document.createElement('canvas')
	const sourceContext = source.getContext('2d')
	let captured: Blob | null = null
	let previewURL: string | null = null
	let strokes: Stroke[] = []
	let openingStrokes: Stroke[] = []
	let drawing: Stroke | null = null
	let activeColor = '#d92d20'

	const updateHistory = () => {
		undoButton.disabled = strokes.length === 0
		clearButton.disabled = strokes.length === 0
	}
	const redraw = () => {
		if (!context || !source.width || !source.height) return
		context.clearRect(0, 0, canvas.width, canvas.height)
		context.drawImage(source, 0, 0)
		for (const stroke of strokes) drawStroke(context, stroke)
		updateHistory()
	}
	const closeEditor = (restore: boolean) => {
		if (restore) strokes = openingStrokes.map(cloneStroke)
		drawing = null
		redraw()
		dialog.close()
		if (!captured) captureButton.focus()
		else editButton.focus()
	}
	const openEditor = () => {
		openingStrokes = strokes.map(cloneStroke)
		redraw()
		dialog.showModal()
		canvas.focus()
	}
	const setSource = async (blob: Blob) => {
		const image = await imageFrom(blob)
		canvas.width = image.naturalWidth
		canvas.height = image.naturalHeight
		source.width = image.naturalWidth
		source.height = image.naturalHeight
		sourceContext?.drawImage(image, 0, 0)
		strokes = []
		redraw()
	}
	const showAttached = (blob: Blob) => {
		if (previewURL) URL.revokeObjectURL(previewURL)
		previewURL = URL.createObjectURL(blob)
		preview.src = previewURL
		captureButton.hidden = true
		summary.hidden = false
	}
	const removeScreenshot = () => {
		captured = null
		strokes = []
		canvas.width = 0
		canvas.height = 0
		source.width = 0
		source.height = 0
		if (previewURL) URL.revokeObjectURL(previewURL)
		previewURL = null
		preview.removeAttribute('src')
		summary.hidden = true
		captureButton.hidden = false
		captureButton.textContent = 'Capture screenshot'
		captureButton.focus()
	}

	captureButton.addEventListener('click', async () => {
		captureButton.disabled = true
		captureButton.textContent = 'Capturing...'
		try {
			const blob = await (capture ?? (() => captureDisplayFrame(root)))()
			if (!blob) throw new Error('screenshot capture returned no image')
			await setSource(blob)
			captureButton.textContent = 'Capture screenshot'
			openEditor()
		} catch {
			captureButton.textContent = 'Screenshot unavailable - retry'
		} finally {
			captureButton.disabled = false
		}
	})

	for (const button of colorButtons) {
		button.addEventListener('click', () => {
			activeColor = button.dataset.color ?? activeColor
			for (const candidate of colorButtons) candidate.setAttribute('aria-pressed', String(candidate === button))
		})
	}
	undoButton.addEventListener('click', () => {
		strokes.pop()
		redraw()
	})
	clearButton.addEventListener('click', () => {
		strokes = []
		redraw()
	})
	editButton.addEventListener('click', openEditor)
	removeButton.addEventListener('click', removeScreenshot)
	closeButton.addEventListener('click', () => closeEditor(true))
	cancelButton.addEventListener('click', () => closeEditor(true))
	dialog.addEventListener('cancel', (event) => {
		event.preventDefault()
		closeEditor(true)
	})
	dialog.addEventListener('click', (event) => {
		if (event.target === dialog) closeEditor(true)
	})
	saveButton.addEventListener('click', async () => {
		saveButton.disabled = true
		try {
			const blob = await canvasBlob(canvas)
			captured = blob
			showAttached(blob)
			closeEditor(false)
		} finally {
			saveButton.disabled = false
		}
	})

	canvas.tabIndex = 0
	canvas.addEventListener('pointerdown', (event) => {
		if (event.button !== 0) return
		event.preventDefault()
		const point = canvasPoint(canvas, event)
		const bounds = canvas.getBoundingClientRect()
		const scale = bounds.width > 0 ? canvas.width / bounds.width : 1
		drawing = { color: activeColor, width: Number(brushSize.value) * scale, points: [point] }
		strokes.push(drawing)
		canvas.setPointerCapture?.(event.pointerId)
		redraw()
	})
	canvas.addEventListener('pointermove', (event) => {
		if (!drawing) return
		drawing.points.push(canvasPoint(canvas, event))
		redraw()
	})
	const finishDrawing = (event: PointerEvent) => {
		if (!drawing) return
		drawing = null
		if (canvas.hasPointerCapture?.(event.pointerId)) canvas.releasePointerCapture(event.pointerId)
	}
	canvas.addEventListener('pointerup', finishDrawing)
	canvas.addEventListener('pointercancel', finishDrawing)

	return {
		element: root,
		screenshot: () => captured,
		destroy: () => {
			if (previewURL) URL.revokeObjectURL(previewURL)
			previewURL = null
			if (dialog.open) dialog.close()
		},
	}
}

async function captureDisplayFrame(control: HTMLElement): Promise<Blob | null> {
	let stream: MediaStream | null = null
	let video: HTMLVideoElement | null = null
	const rootNode = control.getRootNode()
	const excluded = rootNode instanceof ShadowRoot ? rootNode.host as HTMLElement : control
	const previousVisibility = excluded.style.visibility
	try {
		stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
		video = document.createElement('video')
		video.srcObject = stream
		video.muted = true
		await video.play()
		excluded.style.visibility = 'hidden'
		await nextPaint()
		const canvas = document.createElement('canvas')
		canvas.width = video.videoWidth
		canvas.height = video.videoHeight
		canvas.getContext('2d')?.drawImage(video, 0, 0)
		return await canvasBlob(canvas)
	} finally {
		excluded.style.visibility = previousVisibility
		stream?.getTracks().forEach(track => track.stop())
		if (video) video.srcObject = null
	}
}

function drawStroke(context: CanvasRenderingContext2D, stroke: Stroke): void {
	const first = stroke.points[0]
	if (!first) return
	context.save()
	context.strokeStyle = stroke.color
	context.fillStyle = stroke.color
	context.lineWidth = stroke.width
	context.lineCap = 'round'
	context.lineJoin = 'round'
	if (stroke.points.length === 1) {
		context.beginPath()
		context.arc(first.x, first.y, stroke.width / 2, 0, Math.PI * 2)
		context.fill()
	} else {
		context.beginPath()
		context.moveTo(first.x, first.y)
		for (const point of stroke.points.slice(1)) context.lineTo(point.x, point.y)
		context.stroke()
	}
	context.restore()
}

function canvasPoint(canvas: HTMLCanvasElement, event: PointerEvent): Point {
	const bounds = canvas.getBoundingClientRect()
	return {
		x: (event.clientX - bounds.left) * canvas.width / bounds.width,
		y: (event.clientY - bounds.top) * canvas.height / bounds.height,
	}
}

function cloneStroke(stroke: Stroke): Stroke {
	return { ...stroke, points: stroke.points.map(point => ({ ...point })) }
}

function canvasBlob(canvas: HTMLCanvasElement): Promise<Blob> {
	return new Promise((resolve, reject) => canvas.toBlob(
		blob => blob ? resolve(blob) : reject(new Error('could not encode screenshot')),
		'image/png',
	))
}

function imageFrom(blob: Blob): Promise<HTMLImageElement> {
	return new Promise((resolve, reject) => {
		const image = new Image()
		const url = URL.createObjectURL(blob)
		image.onload = () => {
			URL.revokeObjectURL(url)
			resolve(image)
		}
		image.onerror = () => {
			URL.revokeObjectURL(url)
			reject(new Error('could not decode screenshot'))
		}
		image.src = url
	})
}

function nextPaint(): Promise<void> {
	return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
}

function required<T extends Element>(root: ParentNode, selector: string): T {
	const element = root.querySelector<T>(selector)
	if (!element) throw new Error(`screenshot control element missing: ${selector}`)
	return element
}

function controlStyles(): string {
	return `<style>
		.fb-screenshot{font-family:"Avenir Next","Segoe UI",sans-serif;color:var(--fb-fg,#171a19)}
		.fb-screenshot button{font-family:inherit;letter-spacing:0;cursor:pointer}
		.fb-screenshot button:disabled{opacity:.45;cursor:not-allowed}
		.fb-screenshot__capture{width:100%;min-height:38px;padding:0 12px;border:1px solid var(--fb-line,#d9dfdc);border-radius:5px;background:transparent;color:inherit;font-size:13px;font-weight:700}
		.fb-screenshot__capture:hover{border-color:var(--fb-accent,#147d64)}
		.fb-screenshot__summary{display:grid;grid-template-columns:52px minmax(0,1fr);gap:10px;align-items:center;padding:8px;border:1px solid var(--fb-line,#d9dfdc);border-radius:5px;background:color-mix(in srgb,var(--fb-fg,#171a19) 3%,transparent)}
		.fb-screenshot__summary[hidden]{display:none}.fb-screenshot__summary img{width:52px;height:38px;object-fit:cover;border:1px solid var(--fb-line,#d9dfdc);border-radius:3px}.fb-screenshot__summary span{display:flex;min-width:0;flex-direction:column;gap:2px}.fb-screenshot__summary strong{font-size:11px}.fb-screenshot__summary small{overflow:hidden;color:var(--fb-muted,#63706b);font-size:9px;text-overflow:ellipsis;white-space:nowrap}.fb-screenshot__summary>div{grid-column:1/-1;display:flex;gap:6px}.fb-screenshot__summary button{min-height:30px;padding:0 9px;border:1px solid var(--fb-line,#d9dfdc);border-radius:4px;background:transparent;color:inherit;font-size:10px;font-weight:700}.fb-screenshot__summary button:first-child{border-color:var(--fb-accent,#147d64);color:var(--fb-accent,#147d64)}
		.fb-screenshot__dialog{box-sizing:border-box;width:min(920px,calc(100vw - 40px));max-width:none;max-height:calc(100vh - 40px);margin:auto;padding:0;overflow:hidden;border:1px solid #ccd5d1;border-radius:6px;background:#fff;color:#171a19;box-shadow:0 24px 80px rgb(0 0 0/28%)}.fb-screenshot__dialog::backdrop{background:rgb(12 19 17/66%)}.fb-screenshot__dialog>section{display:grid;max-height:calc(100vh - 40px);grid-template-rows:auto minmax(0,1fr) auto auto}.fb-screenshot__dialog header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:14px 16px;border-bottom:1px solid #d9dfdc}.fb-screenshot__dialog header span{display:block;margin-bottom:3px;color:#147d64;font-size:9px;font-weight:800}.fb-screenshot__dialog h3{margin:0;font-size:16px;line-height:1.3}.fb-screenshot__dialog [data-close]{width:32px;height:32px;display:grid;place-items:center;border:0;border-radius:4px;background:transparent;color:#63706b;font-size:23px;line-height:1}.fb-screenshot__dialog [data-close]:hover{background:#edf2f0;color:#171a19}
		.fb-screenshot__stage{display:grid;min-height:220px;place-items:center;padding:16px;overflow:auto;background:#202624}.fb-screenshot__stage canvas{display:block;width:auto;max-width:100%;height:auto;max-height:56vh;touch-action:none;cursor:crosshair;box-shadow:0 8px 30px rgb(0 0 0/28%);outline:none}.fb-screenshot__stage canvas:focus-visible{outline:3px solid #5cc8ad;outline-offset:3px}
		.fb-screenshot__toolbar{display:flex;align-items:end;gap:18px;padding:10px 16px;border-top:1px solid #d9dfdc}.fb-screenshot__toolbar fieldset{margin:0;padding:0;border:0}.fb-screenshot__toolbar legend,.fb-screenshot__toolbar label{display:grid;gap:5px;color:#63706b;font-size:9px;font-weight:700}.fb-screenshot__colors{display:flex;gap:6px}.fb-screenshot__colors button{width:28px;height:28px;border:2px solid transparent;border-radius:50%;background:var(--color)}.fb-screenshot__colors button:nth-child(1){--color:#d92d20}.fb-screenshot__colors button:nth-child(2){--color:#f5b700}.fb-screenshot__colors button:nth-child(3){--color:#2563eb}.fb-screenshot__colors button[aria-pressed=true]{outline:2px solid #171a19;outline-offset:2px}.fb-screenshot__toolbar input{width:130px;accent-color:#147d64}.fb-screenshot__history{display:flex;gap:6px;margin-left:auto}.fb-screenshot__history button,.fb-screenshot__dialog footer button{min-height:34px;padding:0 11px;border:1px solid #cbd4d0;border-radius:4px;background:#fff;color:#26312d;font-size:10px;font-weight:750}.fb-screenshot__dialog footer{display:flex;justify-content:flex-end;gap:8px;padding:10px 16px;border-top:1px solid #d9dfdc;background:#f7f9f8}.fb-screenshot__dialog footer [data-save]{border-color:#147d64;background:#147d64;color:#fff}
		@media(max-width:600px){.fb-screenshot__dialog{width:calc(100vw - 20px);max-height:calc(100vh - 20px)}.fb-screenshot__dialog>section{max-height:calc(100vh - 20px)}.fb-screenshot__stage{min-height:180px;padding:10px}.fb-screenshot__stage canvas{max-height:44vh}.fb-screenshot__toolbar{display:grid;grid-template-columns:1fr 1fr;gap:12px}.fb-screenshot__toolbar input{width:100%}.fb-screenshot__history{grid-column:1/-1;margin:0}.fb-screenshot__history button{flex:1}.fb-screenshot__dialog footer button{flex:1}}
	</style>`
}
