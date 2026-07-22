import { collectMetadata } from './metadata'
import type { FunnelEvent } from './analytics'
import type {
	AttachmentDeclaration,
	AttachmentKind,
	ElementAttachment,
	PendingAttachment,
	SubmissionResponse,
	WidgetConfiguration,
	WidgetRule,
} from './types'

interface MountOptions {
  configuration: WidgetConfiguration
  rule: WidgetRule
  publicKey: string
  apiBase: URL
	assetBase: URL
	replaySessionID: string | null
  onVisible(): void
  onInteraction(): void
  submissionAnalytics(): FunnelEvent | null
  onSubmitted(): void
  onDismissed(): void
}

interface RatingModule {
  createRatingControl(): { element: HTMLElement; value(): number | null }
}

interface ElementModule {
  chooseElement(excludedHost: HTMLElement): Promise<ElementAttachment | null>
}

interface AudioModule {
	createAudioControl(): { element: HTMLElement; recording(): Promise<Blob | null>; destroy(): void }
}

interface VideoModule {
	createVideoControl(): { element: HTMLElement; recording(): Promise<Blob | null>; destroy(): void }
}

interface ScreenshotModule {
	createScreenshotControl(): { element: HTMLElement; screenshot(): Blob | null }
}

interface ErrorModule {
  captureWidgetError(error: unknown, options: { dsn: string; context: Record<string, unknown> }): void
}

export function mountWidget(options: MountOptions): () => void {
  document.getElementById('fidibeki-widget-host')?.remove()
  const host = document.createElement('div')
  host.id = 'fidibeki-widget-host'
  const shadow = host.attachShadow({ mode: 'open' })
  const accent = options.rule.accent_color ?? '#147d64'
  const side = options.rule.position === 'bottom-right' ? 'right' : 'left'
  shadow.innerHTML = `<style>${styles(accent, side, options.rule.theme)}</style>${markup(options.rule)}`
  document.body.append(host)

  const form = requiredElement<HTMLFormElement>(shadow, 'form')
  const closeButton = requiredElement<HTMLButtonElement>(shadow, '[data-close]')
  const submitButton = requiredElement<HTMLButtonElement>(shadow, '[data-submit]')
  const message = requiredElement<HTMLElement>(shadow, '[data-message]')
  const optional = requiredElement<HTMLElement>(shadow, '[data-optional]')
  const text = shadow.querySelector<HTMLTextAreaElement>('textarea[name="text"]')
  const honeypot = requiredElement<HTMLInputElement>(shadow, 'input[name="company_website"]')
  let attachment: ElementAttachment | null = null
  let ratingValue: (() => number | null) | null = null
	let audioRecording: (() => Promise<Blob | null>) | null = null
	let videoRecording: (() => Promise<Blob | null>) | null = null
	let screenshot: (() => Blob | null) | null = null
	let pendingAttachments: PendingAttachment[] | null = null
	let accepted = false
	let submitting = false
	let interactionReported = false
	let analyticsPrepared = false
	let submissionAnalytics: Omit<FunnelEvent, 'event_type'> | null = null
	const clientFeedbackID = randomID()
  const cleanups: Array<() => void> = []
	const reportInteraction = () => {
		if (interactionReported) return
		interactionReported = true
		try {
			options.onInteraction()
		} catch {
			// Analytics must not interfere with feedback input.
		}
	}
	shadow.addEventListener('click', (event) => {
		const target = event.target
		const button = target instanceof Element ? target.closest('button') : null
		if (button && button !== closeButton) reportInteraction()
	})
	text?.addEventListener('input', reportInteraction)

  if (options.rule.can_rate) {
    void import(new URL('rating.js', options.assetBase).href).then((module) => {
      const control = (module as RatingModule).createRatingControl()
      optional.prepend(control.element)
      ratingValue = control.value
    })
  }

  if (options.rule.can_attach_element && options.configuration.features.dom_capture) {
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'secondary'
    button.textContent = 'Attach page element'
    button.addEventListener('click', async () => {
      const module = (await import(new URL('element-attachment.js', options.assetBase).href)) as ElementModule
      host.style.display = 'none'
      attachment = await module.chooseElement(host)
      host.style.display = ''
      button.textContent = attachment ? 'Element attached' : 'Attach page element'
    })
    optional.append(button)
  }

  if (options.rule.can_record_audio && options.configuration.features.audio) {
    void import(new URL('audio.js', options.assetBase).href).then((module) => {
      const control = (module as AudioModule).createAudioControl()
      optional.append(control.element)
			audioRecording = control.recording
      cleanups.push(control.destroy)
    })
  }

  if (options.rule.can_record_video && options.configuration.features.video) {
    void import(new URL('video.js', options.assetBase).href).then((module) => {
      const control = (module as VideoModule).createVideoControl()
      optional.append(control.element)
			videoRecording = control.recording
      cleanups.push(control.destroy)
    })
  }

	if (options.rule.can_attach_screenshot && options.configuration.features.dom_capture) {
		void import(new URL('screenshot.js', options.assetBase).href).then((module) => {
			const control = (module as ScreenshotModule).createScreenshotControl()
			optional.append(control.element)
			screenshot = control.screenshot
		})
	}

  const destroy = () => {
    for (const cleanup of cleanups) cleanup()
    host.remove()
  }
  closeButton.addEventListener('click', () => {
		if (submitting || accepted) return
		options.onDismissed()
    destroy()
  })
  form.addEventListener('submit', async (event) => {
    event.preventDefault()
		if (submitting || accepted) return
		submitting = true
		closeButton.disabled = true
		reportInteraction()
		if (!analyticsPrepared) {
			analyticsPrepared = true
			try {
				const analytics = options.submissionAnalytics()
				if (analytics) {
					const { event_type: _eventType, ...context } = analytics
					submissionAnalytics = context
				}
			} catch {
				// Analytics identity failures must not block feedback submission.
			}
		}
    submitButton.disabled = true
    message.textContent = 'Sending...'
    try {
			pendingAttachments ??= await collectAttachments({ audioRecording, videoRecording, screenshot })
			const feedbackURL = endpointURL(options, 'feedback')
			const response = await fetch(feedbackURL, {
        method: 'POST',
        mode: 'cors',
        credentials: 'omit',
        headers: {
          'Content-Type': 'application/json',
          'X-Fidibeki-Public-Key': options.publicKey,
        },
        body: JSON.stringify({
				client_feedback_id: clientFeedbackID,
          widget_rule_id: options.rule.id,
			session_id: options.replaySessionID,
          rating: ratingValue?.() ?? null,
          text: text?.value.trim() || null,
          element_attachment: attachment,
          company_website: honeypot.value,
          metadata: collectMetadata(),
				attachments: pendingAttachments.map(declarationFor),
				analytics: submissionAnalytics,
        }),
      })
      if (!response.ok) throw new Error(`feedback submission failed with status ${response.status}`)
      accepted = true
		submitting = false
      try {
        options.onSubmitted()
      } catch {
        // Host storage failures must not turn an accepted submission into an error.
      }
      try {
			  const submission = await response.json() as SubmissionResponse
			  message.textContent = pendingAttachments.length > 0 ? 'Uploading attachments...' : ''
			  await uploadAttachments(options, pendingAttachments, submission.attachments ?? [])
        form.innerHTML = '<div class="success" role="status"><strong>Thank you.</strong><span>Your feedback was sent.</span></div>'
        window.setTimeout(destroy, 1800)
      } catch (error) {
        form.innerHTML = '<div class="success" role="status"><strong>Feedback received.</strong><span>One or more attachments could not be uploaded.</span></div>'
        window.setTimeout(destroy, 3200)
        void reportError(options, error, 'attachments')
      }
    } catch (error) {
		submitting = false
      message.textContent = 'Could not send feedback. Please try again.'
      submitButton.disabled = false
		closeButton.disabled = false
      void reportError(options, error, 'submission')
    }
  })

	try {
		options.onVisible()
	} catch {
		// Analytics must not interfere with the visible widget.
	}

  return destroy
}

async function collectAttachments(sources: {
	audioRecording: (() => Promise<Blob | null>) | null
	videoRecording: (() => Promise<Blob | null>) | null
	screenshot: (() => Blob | null) | null
}): Promise<PendingAttachment[]> {
	const pending: PendingAttachment[] = []
	const audio = await sources.audioRecording?.()
	if (audio && audio.size > 0) pending.push(pendingAttachment('audio', audio))
	const video = await sources.videoRecording?.()
	if (video && video.size > 0) pending.push(pendingAttachment('video', video))
	const screenshot = sources.screenshot?.()
	if (screenshot && screenshot.size > 0) pending.push(pendingAttachment('screenshot', screenshot))
	return pending
}

function pendingAttachment(kind: AttachmentKind, blob: Blob): PendingAttachment {
	return { clientAttachmentID: randomID(), kind, blob }
}

function declarationFor(attachment: PendingAttachment): AttachmentDeclaration {
	return {
		client_attachment_id: attachment.clientAttachmentID,
		kind: attachment.kind,
		content_type: normalizedContentType(attachment),
		size_bytes: attachment.blob.size,
	}
}

function normalizedContentType(attachment: PendingAttachment): string {
	const declared = attachment.blob.type.split(';', 1)[0]?.trim().toLowerCase()
	if (declared) return declared
	if (attachment.kind === 'screenshot') return 'image/png'
	return `${attachment.kind}/webm`
}

async function uploadAttachments(
	options: MountOptions,
	pending: PendingAttachment[],
	uploads: NonNullable<SubmissionResponse['attachments']>,
): Promise<void> {
	const byClientID = new Map(pending.map((attachment) => [attachment.clientAttachmentID, attachment]))
	await Promise.all(uploads.map(async (upload) => {
		const attachment = byClientID.get(upload.client_attachment_id)
		if (!attachment) throw new Error('feedback response referenced an unknown attachment')
		const uploaded = await fetch(upload.presigned_url, {
			method: upload.method,
			headers: upload.headers,
			body: attachment.blob,
			credentials: 'omit',
		})
		if (!uploaded.ok) throw new Error(`attachment upload failed with status ${uploaded.status}`)
		const completed = await fetch(endpointURL(options, `attachments/${upload.attachment_id}/complete`), {
			method: 'POST', mode: 'cors', credentials: 'omit',
			headers: {
				'Content-Type': 'application/json',
				'X-Fidibeki-Public-Key': options.publicKey,
			},
			body: JSON.stringify({ complete_token: upload.complete_token }),
		})
		if (!completed.ok) throw new Error(`attachment completion failed with status ${completed.status}`)
	}))
}

function endpointURL(options: MountOptions, path: string): URL {
	const url = new URL(path, options.apiBase)
	url.searchParams.set('key', options.publicKey)
	return url
}

async function reportError(options: MountOptions, error: unknown, phase: string): Promise<void> {
  const dsn = options.configuration.error_reporting_dsn
  if (!dsn) return
  try {
    const module = (await import(new URL('error-reporting.js', options.assetBase).href)) as ErrorModule
    module.captureWidgetError(error, {
      dsn,
      context: { phase, project_id: options.configuration.project_id, widget_rule_id: options.rule.id },
    })
  } catch {
    // Error reporting must never interfere with feedback retries.
  }
}

function markup(rule: WidgetRule): string {
  return `<section class="panel" aria-label="Feedback form">
    <header><div><span class="eyebrow">FIDIBEKI</span><h2>${escapeHTML(rule.greeting ?? 'How was this page?')}</h2></div>
      <button class="icon" type="button" data-close aria-label="Close feedback" title="Close">&#215;</button></header>
    <form>
      <div data-optional class="optional"></div>
      ${rule.can_write_text ? '<label><span>Feedback</span><textarea name="text" rows="4" maxlength="4000" placeholder="Tell us what worked or what got in the way"></textarea></label>' : ''}
      <label class="honeypot" aria-hidden="true">Website<input name="company_website" tabindex="-1" autocomplete="off"></label>
      <footer><span data-message aria-live="polite"></span><button class="primary" type="submit" data-submit>Send feedback</button></footer>
    </form>
  </section>`
}

function styles(accent: string, side: 'left' | 'right', theme: WidgetRule['theme']): string {
  const dark = theme === 'dark'
  const automatic = theme === 'auto'
  return `:host{--fb-accent:${accent};--fb-bg:${dark ? '#171918' : '#fff'};--fb-fg:${dark ? '#f5f7f6' : '#171a19'};--fb-muted:${dark ? '#aeb8b4' : '#63706b'};--fb-line:${dark ? '#3b413e' : '#d9dfdc'};all:initial;font-family:"Avenir Next","Segoe UI",sans-serif;color:var(--fb-fg)}
  .panel{position:fixed;z-index:2147483000;bottom:20px;${side}:20px;width:min(368px,calc(100vw - 32px));box-sizing:border-box;background:var(--fb-bg);color:var(--fb-fg);border:1px solid var(--fb-line);border-top:4px solid var(--fb-accent);border-radius:6px;box-shadow:0 18px 50px rgba(0,0,0,.2);overflow:hidden}
  header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:20px 20px 14px}h2{font:700 18px/1.3 "Avenir Next","Segoe UI",sans-serif;margin:3px 0 0;letter-spacing:0}.eyebrow{font:700 10px/1 sans-serif;color:var(--fb-accent);letter-spacing:0}form{padding:0 20px 20px}.icon{width:32px;height:32px;display:grid;place-items:center;border:0;background:transparent;color:var(--fb-muted);font-size:24px;line-height:1;cursor:pointer}.icon:hover{color:var(--fb-fg);background:color-mix(in srgb,var(--fb-fg) 7%,transparent)}.icon:disabled{opacity:.5;cursor:wait}label>span{display:block;margin:14px 0 7px;font:650 12px/1.2 sans-serif;color:var(--fb-muted)}textarea{box-sizing:border-box;width:100%;resize:vertical;min-height:92px;padding:11px 12px;border:1px solid var(--fb-line);border-radius:5px;background:var(--fb-bg);color:var(--fb-fg);font:400 14px/1.45 "Avenir Next","Segoe UI",sans-serif;letter-spacing:0;outline:none}textarea:focus{border-color:var(--fb-accent);box-shadow:0 0 0 2px color-mix(in srgb,var(--fb-accent) 22%,transparent)}.optional{display:grid;gap:10px}.primary,.secondary{min-height:38px;border-radius:5px;font:700 13px/1 sans-serif;letter-spacing:0;cursor:pointer}.primary{border:1px solid var(--fb-accent);background:var(--fb-accent);color:#fff;padding:0 15px}.primary:disabled{opacity:.6;cursor:wait}.secondary{width:100%;border:1px solid var(--fb-line);background:transparent;color:var(--fb-fg);padding:0 12px}.secondary:hover{border-color:var(--fb-accent)}footer{display:flex;align-items:center;justify-content:flex-end;gap:12px;margin-top:16px}footer span{flex:1;color:var(--fb-muted);font:500 11px/1.3 sans-serif}.honeypot{position:absolute!important;left:-10000px!important;width:1px!important;height:1px!important;overflow:hidden!important}.success{display:grid;gap:6px;padding:22px 0 8px;text-align:center}.success strong{font:700 18px/1.2 sans-serif}.success span{font:400 13px/1.4 sans-serif;color:var(--fb-muted)}
  ${automatic ? '@media(prefers-color-scheme:dark){:host{--fb-bg:#171918;--fb-fg:#f5f7f6;--fb-muted:#aeb8b4;--fb-line:#3b413e}}' : ''}
  @media(max-width:480px){.panel{bottom:12px;left:12px;right:12px;width:auto}}
  @media(prefers-reduced-motion:no-preference){.panel{animation:fb-enter .22s ease-out both}@keyframes fb-enter{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:none}}}`
}

function requiredElement<T extends Element>(root: ShadowRoot, selector: string): T {
  const element = root.querySelector<T>(selector)
  if (!element) throw new Error(`Fidibeki widget element missing: ${selector}`)
  return element
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>'"]/g, (character) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;',
  })[character] ?? character)
}

function randomID(): string {
  const cryptoObject = globalThis.crypto as Crypto & { randomUUID?: () => string }
  if (typeof cryptoObject.randomUUID === 'function') return cryptoObject.randomUUID()
  const bytes = cryptoObject.getRandomValues(new Uint8Array(16))
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80
  const hex = Array.from(bytes).map((byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
