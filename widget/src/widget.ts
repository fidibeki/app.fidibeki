import { collectMetadata } from './metadata'
import type { FunnelEvent } from './analytics'
import {
  resolveWidgetLocalizationForPage,
  type ResolvedWidgetLocalization,
  type WidgetSystemCopy,
} from './localization'
import { readPageLanguage, subscribeToPageLanguage } from './page-language'
import {
	AttachmentPipelineError,
	PublicAPIResponseError,
	parsePublicAPIError,
	publicFailureMessage,
	type AttachmentFailure,
	type PublicAPIError,
} from './submission-errors'
import type {
	AttachmentDeclaration,
	AttachmentKind,
	ElementAttachment,
	PendingAttachment,
	SubmissionResponse,
	WidgetConfiguration,
	WidgetResponseBlock,
	WidgetRule,
} from './types'
import { resolveAccentForeground } from './appearance'

export const REPORTER_CONSENT_PURPOSE = 'feedback_updates'
export const REPORTER_CONSENT_NOTICE_VERSION = '2026-07-controller-v1'

interface MountOptions {
  configuration: WidgetConfiguration
  rule: WidgetRule
  publicKey: string
  apiBase: URL
	assetBase: URL
  pageURL: string
	replaySessionID: string | null
  onVisible(): void
  onInteraction(): void
  submissionAnalytics(): FunnelEvent | null
  onSubmitted(): void
  onDismissed(): void
  target?: HTMLElement
  hostID?: string
}

interface RatingModule {
  createRatingControl(copy: Pick<WidgetSystemCopy, 'rating' | 'ratingOutOfFive'>): {
    element: HTMLElement
    value(): number | null
    localize(copy: Pick<WidgetSystemCopy, 'rating' | 'ratingOutOfFive'>): void
  }
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
	createScreenshotControl(): { element: HTMLElement; screenshot(): Blob | null; destroy?(): void }
}

interface ErrorModule {
  captureWidgetError(error: unknown, options: { dsn: string; context: Record<string, unknown> }): void
}

export function mountWidget(options: MountOptions): () => void {
  const hostID = options.hostID ?? 'fidibeki-widget-host'
  document.getElementById(hostID)?.remove()
  const host = document.createElement('div')
  host.id = hostID
  const shadow = host.attachShadow({ mode: 'open' })
  const appearance = options.rule.appearance
  const accent = appearance?.accent_color ?? options.rule.accent_color ?? '#147d64'
  const accentForeground = appearance?.accent_foreground ?? resolveAccentForeground(accent)
  const side = (appearance?.position ?? options.rule.position) === 'bottom-right' ? 'right' : 'left'
  const browserLocales = navigator.languages.length > 0 ? navigator.languages : [navigator.language]
  let localization = resolveWidgetLocalizationForPage(
    options.rule,
    options.configuration.default_locale,
    readPageLanguage(),
    browserLocales,
  )
  const presentation = options.rule.presentation ?? 'prompt'
  const structured = options.rule.presentation !== undefined && (options.rule.response_blocks?.length ?? 0) > 0
  shadow.innerHTML = `<style>${styles(
    accent, accentForeground, side, appearance?.theme ?? options.rule.theme,
    appearance?.surface_color, appearance?.text_color,
  )}</style>${markup(
    options.rule, localization, presentation, structured, appearance?.logo_url,
    options.configuration.controller_profile,
  )}`
  ;(options.target ?? document.body).append(host)

  const form = requiredElement<HTMLFormElement>(shadow, 'form')
  const closeButton = requiredElement<HTMLButtonElement>(shadow, '[data-close]')
  const submitButton = requiredElement<HTMLButtonElement>(shadow, '[data-submit]')
  const message = requiredElement<HTMLElement>(shadow, '[data-message]')
  const optional = requiredElement<HTMLElement>(shadow, '[data-optional]')
  const text = shadow.querySelector<HTMLTextAreaElement>('textarea[name="text"]')
  const comment = shadow.querySelector<HTMLTextAreaElement>('textarea[name="comment"]')
  const reporterEmail = shadow.querySelector<HTMLInputElement>('input[name="reporter_email"]')
  const notifyReporter = shadow.querySelector<HTMLInputElement>('input[name="notify_reporter"]')
  const contactDetail = shadow.querySelector<HTMLElement>('[data-contact]')
  const launcher = shadow.querySelector<HTMLButtonElement>('[data-launcher]')
  const panel = requiredElement<HTMLElement>(shadow, '[data-panel]')
  const honeypot = requiredElement<HTMLInputElement>(shadow, 'input[name="company_website"]')
  let attachment: ElementAttachment | null = null
  let ratingValue: (() => number | null) | null = null
  let localizeRating: ((copy: Pick<WidgetSystemCopy, 'rating' | 'ratingOutOfFive'>) => void) | null = null
  let elementAttachmentButton: HTMLButtonElement | null = null
	let audioRecording: (() => Promise<Blob | null>) | null = null
	let videoRecording: (() => Promise<Blob | null>) | null = null
	let screenshot: (() => Blob | null) | null = null
	let pendingAttachments: PendingAttachment[] | null = null
	let accepted = false
	let submitting = false
	let retryTimer: number | null = null
	let interactionReported = false
	let analyticsPrepared = false
	let submissionAnalytics: Omit<FunnelEvent, 'event_type'> | null = null
	const clientFeedbackID = randomID()
  const cleanups: Array<() => void> = []
  const responseControls = structured
    ? createResponseControls(optional, options.rule, localization)
    : null
  type MessageCopy = ((current: ResolvedWidgetLocalization) => string) | null
  let messageCopy: MessageCopy = null
  let submitCopy: 'default' | 'retry' = 'default'
  const renderMessage = () => {
    message.textContent = messageCopy?.(localization) ?? ''
  }
  const setMessage = (copy: MessageCopy) => {
    messageCopy = copy
    renderMessage()
  }
  const applyLocalization = (next: ResolvedWidgetLocalization) => {
    localization = next
    const content = localization.content
    if (launcher) launcher.textContent = content.launcher_label
    requiredElement<HTMLElement>(shadow, '[data-copy="prompt"]').textContent = content.prompt
    panel.setAttribute('aria-label', localization.system.formLabel)
    closeButton.setAttribute('aria-label', localization.system.close)
    closeButton.title = localization.system.close
    const legacyLabel = shadow.querySelector<HTMLElement>('[data-copy="legacy-label"]')
    if (legacyLabel) legacyLabel.textContent = localization.system.feedback
    if (text) text.placeholder = localization.system.feedbackPlaceholder
    const commentLabel = shadow.querySelector<HTMLElement>('[data-copy="comment-label"]')
    if (commentLabel) commentLabel.textContent = content.comment_placeholder
    if (comment) comment.placeholder = content.comment_placeholder
    const contactLabel = shadow.querySelector<HTMLElement>('[data-copy="contact-label"]')
    if (contactLabel) contactLabel.textContent = content.contact_label
    if (reporterEmail) reporterEmail.placeholder = localization.system.emailPlaceholder
    const consentLabel = shadow.querySelector<HTMLElement>('[data-copy="consent-label"]')
    if (consentLabel) consentLabel.textContent = content.consent_label
    const consentNotice = shadow.querySelector<HTMLElement>('[data-copy="consent-notice"]')
    if (consentNotice) consentNotice.textContent = localization.system.consentNotice
    const controllerSummary = shadow.querySelector<HTMLElement>('[data-copy="controller-summary"]')
    if (controllerSummary && options.configuration.controller_profile) {
      controllerSummary.textContent = localization.system.controllerNotice.replace(
        '{controller}',
        options.configuration.controller_profile.name,
      )
    }
    const purposeSummary = shadow.querySelector<HTMLElement>('[data-copy="privacy-purpose"]')
    if (purposeSummary) purposeSummary.textContent = content.privacy_purpose_summary ?? ''
    const lawfulBasis = shadow.querySelector<HTMLElement>('[data-copy="privacy-lawful-basis"]')
    if (lawfulBasis) {
      lawfulBasis.textContent = `${localization.system.lawfulBasis}: ${content.privacy_lawful_basis_summary ?? ''}`
    }
    const retention = shadow.querySelector<HTMLElement>('[data-copy="privacy-retention"]')
    if (retention) {
      retention.textContent = `${localization.system.retention}: ${content.privacy_retention_summary ?? ''}`
    }
    const privacyContact = shadow.querySelector<HTMLElement>('[data-copy="privacy-contact"]')
    if (privacyContact) privacyContact.textContent = localization.system.privacyContact
    const rightsContact = shadow.querySelector<HTMLElement>('[data-copy="rights-contact"]')
    if (rightsContact) rightsContact.textContent = localization.system.rightsContact
    const privacyLink = shadow.querySelector<HTMLAnchorElement>('[data-copy="privacy-link"]')
    if (privacyLink) privacyLink.textContent = localization.system.privacyPolicy
    const aiDisclosure = shadow.querySelector<HTMLElement>('[data-copy="ai-disclosure"]')
    if (aiDisclosure) aiDisclosure.textContent = aiDisclosureCopy(options.rule, localization)
    const aiPrivacyLink = shadow.querySelector<HTMLAnchorElement>('[data-copy="ai-privacy-link"]')
    if (aiPrivacyLink) aiPrivacyLink.textContent = localization.system.privacyPolicy
    submitButton.textContent = submitCopy === 'retry' ? localization.system.retry : content.submit_label
    responseControls?.localize(localization)
    localizeRating?.(localization.system)
    if (elementAttachmentButton) {
      elementAttachmentButton.textContent = attachment
        ? localization.system.elementAttached
        : localization.system.attachElement
    }
    renderMessage()
  }
  cleanups.push(subscribeToPageLanguage((pageLanguage) => {
    applyLocalization(resolveWidgetLocalizationForPage(
      options.rule,
      options.configuration.default_locale,
      pageLanguage,
      browserLocales,
    ))
  }))
	const messageID = `${hostID}-message`
	message.id = messageID
	if (reporterEmail && notifyReporter) {
		reporterEmail.setAttribute('aria-describedby', messageID)
		const clearContactError = () => {
			if (reporterEmail.getAttribute('aria-invalid') !== 'true') return
			reporterEmail.removeAttribute('aria-invalid')
			reporterEmail.removeAttribute('data-invalid')
			setMessage(null)
		}
		const synchronizeContactRequirement = () => {
			reporterEmail.required = notifyReporter.checked
			if (contactDetail) contactDetail.hidden = !notifyReporter.checked
		}
		reporterEmail.addEventListener('input', clearContactError)
		notifyReporter.addEventListener('change', () => {
			if (!notifyReporter.checked) {
				// Unchecking withdraws opt-in immediately: clear any typed email so it can never
				// reach the submission payload, matching the "unchecking clears email" contract.
				reporterEmail.value = ''
				clearContactError()
			}
			synchronizeContactRequirement()
		})
		synchronizeContactRequirement()
	}
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
	form.addEventListener('input', reportInteraction)

  if (!structured && options.rule.can_rate) {
    void import(new URL('rating.js', options.assetBase).href).then((module) => {
      const control = (module as RatingModule).createRatingControl(localization.system)
      optional.prepend(control.element)
      ratingValue = control.value
      localizeRating = control.localize
    })
  }

  launcher?.addEventListener('click', () => {
		reportInteraction()
		launcher.hidden = true
		panel.hidden = false
		panel.querySelector<HTMLElement>('button,textarea,select,input')?.focus()
	})

  if (options.rule.can_attach_element && options.configuration.features.dom_capture) {
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'secondary'
    button.textContent = localization.system.attachElement
    elementAttachmentButton = button
    button.addEventListener('click', async () => {
      const module = (await import(new URL('element-attachment.js', options.assetBase).href)) as ElementModule
      host.style.display = 'none'
      attachment = await module.chooseElement(host)
      host.style.display = ''
      button.textContent = attachment
        ? localization.system.elementAttached
        : localization.system.attachElement
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
			if (control.destroy) cleanups.push(control.destroy)
		})
	}

  const destroy = () => {
		if (retryTimer !== null) window.clearInterval(retryTimer)
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
    if (responseControls && !responseControls.valid()) {
      setMessage(current => current.system.required)
      responseControls.focusInvalid()
      return
    }
    if (reporterEmail && notifyReporter) {
      const email = reporterEmail.value.trim()
      const contactError = notifyReporter.checked && !email
        ? 'emailRequired'
        : email && reporterEmail.validity.typeMismatch
          ? 'invalidEmail'
          : null
      if (contactError) {
        setMessage(current => current.system[contactError])
        reporterEmail.setAttribute('aria-invalid', 'true')
        reporterEmail.setAttribute('data-invalid', 'true')
        reporterEmail.focus()
        return
      }
    }
    submitting = true
    submitCopy = 'default'
    submitButton.textContent = localization.content.submit_label
    if (retryTimer !== null) {
      window.clearInterval(retryTimer)
      retryTimer = null
    }
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
    setMessage(current => current.system.sending)
    try {
      pendingAttachments ??= await collectAttachments({ audioRecording, videoRecording, screenshot })
      const legacyRating = responseControls?.rating() ?? ratingValue?.() ?? null
      const legacyText = (responseControls?.text() ?? text?.value.trim()) || null
      const responses = responseControls?.values() ?? legacyResponses(legacyRating, legacyText)
      const consentGranted = notifyReporter?.checked ?? false
      const consentEmail = consentGranted ? (reporterEmail?.value.trim() || null) : null
      const consent = consentGranted ? {
        purpose: REPORTER_CONSENT_PURPOSE,
        notice_version: REPORTER_CONSENT_NOTICE_VERSION,
        controller_profile_version: options.configuration.controller_profile?.version ?? 0,
        widget_definition_version: options.rule.definition_version ?? 0,
        locale: localization.locale,
        granted: true,
      } : null
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
          rating: legacyRating,
          text: legacyText,
          responses,
          comment: comment?.value.trim() || null,
          reporter_email: consentEmail,
          notify_reporter: consentGranted,
          consent,
          element_attachment: attachment,
          company_website: honeypot.value,
          metadata: { ...collectMetadata(options.pageURL), language: localization.locale },
          attachments: pendingAttachments.map(declarationFor),
          analytics: submissionAnalytics,
        }),
      })
      if (!response.ok) {
        throw new PublicAPIResponseError(await parsePublicAPIError(response))
      }
      accepted = true
      submitting = false
      try {
        options.onSubmitted()
      } catch {
        // Host storage failures must not turn an accepted submission into an error.
      }
      try {
        const submission = await response.json() as SubmissionResponse
        const attachmentWarning = submission.warnings?.find(warning =>
          warning.reason === 'upload_rate_limited' || warning.code === 'service_unavailable')
        if (pendingAttachments.length > (submission.attachments?.length ?? 0) && !attachmentWarning) {
          throw new Error('feedback response omitted attachment upload grants')
        }
        setMessage(pendingAttachments.length > 0 ? current => current.system.sending : null)
        await uploadAttachments(options, pendingAttachments, submission.attachments ?? [])
        const warningDetail = attachmentWarning
          ? `<span>${escapeHTML(attachmentWarning.message || localization.system.attachmentError)}</span>`
          : ''
        form.innerHTML = `<div class="success" role="status"><strong>${escapeHTML(localization.content.success_message)}</strong>${warningDetail}</div>`
        window.setTimeout(destroy, attachmentWarning ? 3200 : 1800)
      } catch (error) {
        const publicFailure = error instanceof PublicAPIResponseError
          ? error.failure
          : error instanceof AttachmentPipelineError
            ? error.primaryPublicFailure
            : null
        const detail = publicFailure
          ? publicFailureMessage(publicFailure, localization.system)
          : localization.system.attachmentError
        form.innerHTML = `<div class="success" role="status"><strong>${escapeHTML(structured ? localization.content.success_message : 'Feedback received.')}</strong><span>${escapeHTML(detail)}</span></div>`
        window.setTimeout(destroy, 3200)
        void reportError(options, error, 'attachments')
      }
    } catch (error) {
      if (error instanceof PublicAPIResponseError && error.failure.reason) {
        showSubmissionFailure(error.failure)
      } else {
        submitting = false
        setMessage(current => current.system.sendError)
        submitButton.disabled = false
        closeButton.disabled = false
      }
      void reportError(options, error, 'submission')
    }
  })

  const showSubmissionFailure = (failure: PublicAPIError) => {
    submitting = false
    closeButton.disabled = false
    submitCopy = 'retry'
    submitButton.textContent = localization.system.retry
    if (failure.retryAfterSeconds <= 0) {
      setMessage(current => publicFailureMessage(failure, current.system))
      submitButton.disabled = false
      return
    }
    const retryAt = Date.now() + failure.retryAfterSeconds * 1000
    const updateCountdown = () => {
      const remaining = Math.max(0, Math.ceil((retryAt - Date.now()) / 1000))
      setMessage(current => publicFailureMessage(failure, current.system, remaining))
      submitButton.disabled = remaining > 0
      if (remaining > 0 || retryTimer === null) return
      window.clearInterval(retryTimer)
      retryTimer = null
    }
    updateCountdown()
    retryTimer = window.setInterval(updateCountdown, 250)
  }

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
	// allSettled (not all/race) so one failed attachment's upload/completion never aborts or
	// leaves the outcome of the others unobserved: every declared attachment is either uploaded
	// and completed, or reported as a specific per-kind, per-stage failure below.
	const outcomes = await Promise.allSettled(uploads.map(async (upload) => {
		const attachment = byClientID.get(upload.client_attachment_id)
		if (!attachment) {
			throw {
				kind: 'unknown', clientAttachmentId: upload.client_attachment_id, stage: 'upload',
				error: new Error('feedback response referenced an unknown attachment'),
			} satisfies AttachmentFailure
		}
		let uploaded: Response
		try {
			uploaded = await fetch(upload.presigned_url, {
				method: upload.method,
				headers: upload.headers,
				body: attachment.blob,
				credentials: 'omit',
			})
		} catch (error) {
			throw attachmentFailure(attachment, 'upload', error)
		}
		if (!uploaded.ok) {
			throw attachmentFailure(attachment, 'upload', new Error(`attachment upload failed with status ${uploaded.status}`))
		}
		let completed: Response
		try {
			completed = await fetch(endpointURL(options, `attachments/${upload.attachment_id}/complete`), {
				method: 'POST', mode: 'cors', credentials: 'omit',
				headers: {
					'Content-Type': 'application/json',
					'X-Fidibeki-Public-Key': options.publicKey,
				},
				body: JSON.stringify({ complete_token: upload.complete_token }),
			})
		} catch (error) {
			throw attachmentFailure(attachment, 'complete', error)
		}
		if (!completed.ok) {
			throw attachmentFailure(attachment, 'complete', new PublicAPIResponseError(await parsePublicAPIError(completed)))
		}
	}))
	const failures = outcomes
		.filter((outcome): outcome is PromiseRejectedResult => outcome.status === 'rejected')
		.map((outcome) => outcome.reason as AttachmentFailure)
	if (failures.length > 0) throw new AttachmentPipelineError(failures)
}

function attachmentFailure(
	attachment: PendingAttachment,
	stage: AttachmentFailure['stage'],
	error: unknown,
): AttachmentFailure {
	return { kind: attachment.kind, clientAttachmentId: attachment.clientAttachmentID, stage, error }
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

interface ResponseControls {
  values(): Record<string, string | number>
  rating(): number | null
  text(): string | null
  valid(): boolean
  focusInvalid(): void
  localize(localization: ResolvedWidgetLocalization): void
}

interface ResponseControl {
  block: WidgetResponseBlock
  focus: HTMLElement
  container: HTMLElement
  value(): string | number | null
  localize(localization: ResolvedWidgetLocalization): void
}

function createResponseControls(
  container: HTMLElement,
  rule: WidgetRule,
  localization: ResolvedWidgetLocalization,
): ResponseControls {
  const controls = (rule.response_blocks ?? []).map((block) => {
    const control = responseControl(block, localization)
    container.append(control.container)
    return control
  })
  const invalidControls = () => controls.filter(({ block, value }) => block.required && emptyResponse(value()))
  return {
    values() {
      return Object.fromEntries(controls.flatMap(({ block, value }) => {
        const response = value()
        return emptyResponse(response) ? [] : [[block.key, response as string | number]]
      }))
    },
    rating() {
      const value = controls.find(control => control.block.type === 'rating')?.value()
      return typeof value === 'number' ? value : null
    },
    text() {
      const value = controls.find(control => control.block.type === 'free_text')?.value()
      return typeof value === 'string' && value.trim() ? value.trim() : null
    },
    valid() {
      for (const control of controls) control.container.removeAttribute('data-invalid')
      const invalid = invalidControls()
      for (const control of invalid) control.container.setAttribute('data-invalid', 'true')
      return invalid.length === 0
    },
    focusInvalid() { invalidControls()[0]?.focus.focus() },
    localize(next) {
      for (const control of controls) control.localize(next)
    },
  }
}

function responseControl(
  block: WidgetResponseBlock,
  localization: ResolvedWidgetLocalization,
): ResponseControl {
  const label = localization.content.block_labels?.[block.key] ?? defaultBlockLabel(block, localization)
  if (block.type === 'free_text') {
    const wrapper = document.createElement('label')
    const caption = document.createElement('span')
    caption.textContent = label
    const textarea = document.createElement('textarea')
    textarea.rows = 4
    textarea.maxLength = 10000
    textarea.required = block.required
    textarea.name = `response_${block.key}`
    wrapper.append(caption, textarea)
    return {
      block,
      container: wrapper,
      focus: textarea,
      value: () => textarea.value.trim() || null,
      localize: next => {
        caption.textContent = next.content.block_labels?.[block.key] ?? defaultBlockLabel(block, next)
      },
    }
  }
  if (block.type === 'single_choice') {
    const wrapper = document.createElement('label')
    const caption = document.createElement('span')
    caption.textContent = label
    const select = document.createElement('select')
    select.name = `response_${block.key}`
    select.required = block.required
    const placeholder = document.createElement('option')
    placeholder.value = ''
    placeholder.textContent = '—'
    select.append(placeholder)
    for (const option of block.options ?? []) {
      const element = document.createElement('option')
      element.value = option.value
      element.textContent = optionLabel(localization, block.key, option.value)
      select.append(element)
    }
    wrapper.append(caption, select)
    return {
      block,
      container: wrapper,
      focus: select,
      value: () => select.value || null,
      localize: next => {
        caption.textContent = next.content.block_labels?.[block.key] ?? defaultBlockLabel(block, next)
        for (const option of Array.from(select.options).slice(1)) {
          option.textContent = optionLabel(next, block.key, option.value)
        }
      },
    }
  }

  let selected: string | number | null = null
  const fieldset = document.createElement('fieldset')
  const legend = document.createElement('legend')
  legend.textContent = label
  const row = document.createElement('div')
  row.className = block.type === 'rating' ? 'scale' : 'choices reactions'
  const options = block.type === 'rating'
    ? [1, 2, 3, 4, 5].map(value => ({
        value: String(value),
        label: '★',
        title: localization.system.ratingOutOfFive.replace('{value}', String(value)),
      }))
    : (block.options ?? []).map(option => ({
        value: option.value,
        label: optionLabel(localization, block.key, option.value),
        title: optionLabel(localization, block.key, option.value),
      }))
  for (const option of options) {
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'choice'
    button.textContent = option.label
    button.title = option.title
    button.setAttribute('aria-label', option.title)
    button.setAttribute('aria-pressed', 'false')
    button.addEventListener('click', () => {
      selected = block.type === 'rating' ? Number(option.value) : option.value
      for (const candidate of Array.from(row.querySelectorAll<HTMLButtonElement>('button'))) {
        candidate.setAttribute('aria-pressed', String(candidate === button))
      }
    })
    row.append(button)
  }
  fieldset.append(legend, row)
  return {
    block,
    container: fieldset,
    focus: row.querySelector('button') ?? fieldset,
    value: () => selected,
    localize(next) {
      legend.textContent = next.content.block_labels?.[block.key] ?? defaultBlockLabel(block, next)
      for (const [index, button] of Array.from(row.querySelectorAll<HTMLButtonElement>('button')).entries()) {
        const value = block.type === 'rating'
          ? String(index + 1)
          : block.options?.[index]?.value ?? ''
        const title = block.type === 'rating'
          ? next.system.ratingOutOfFive.replace('{value}', value)
          : optionLabel(next, block.key, value)
        if (block.type !== 'rating') button.textContent = title
        button.title = title
        button.setAttribute('aria-label', title)
      }
    },
  }
}

function defaultBlockLabel(block: WidgetResponseBlock, localization: ResolvedWidgetLocalization): string {
  if (block.type === 'rating') return localization.system.rating
  if (block.type === 'free_text') return localization.system.feedback
  if (block.type === 'reaction') return localization.content.prompt
  return localization.system.feedback
}

function optionLabel(localization: ResolvedWidgetLocalization, blockKey: string, value: string): string {
  return localization.content.option_labels?.[blockKey]?.[value]
    ?? value.replace(/_/g, ' ').replace(/^./, first => first.toUpperCase())
}

function emptyResponse(value: string | number | null): boolean {
  return value === null || value === ''
}

function legacyResponses(rating: number | null, text: string | null): Record<string, string | number> {
  return {
    ...(rating === null ? {} : { rating }),
    ...(text === null ? {} : { feedback: text }),
  }
}

function markup(
  rule: WidgetRule,
  localization: ResolvedWidgetLocalization,
  presentation: NonNullable<WidgetRule['presentation']>,
  structured: boolean,
  logoURL?: string,
  controllerProfile?: WidgetConfiguration['controller_profile'],
): string {
  const content = localization.content
  const launcher = presentation === 'launcher'
    ? `<button class="launcher" type="button" data-launcher data-copy="launcher">${escapeHTML(content.launcher_label)}</button>`
    : ''
  const panelHidden = presentation === 'launcher' ? ' hidden' : ''
  const legacyText = !structured && rule.can_write_text
    ? `<label><span data-copy="legacy-label">${escapeHTML(localization.system.feedback)}</span><textarea name="text" rows="4" maxlength="4000" placeholder="${escapeHTML(localization.system.feedbackPlaceholder)}"></textarea></label>`
    : ''
  const comment = structured && rule.allow_comment
    ? `<label><span data-copy="comment-label">${escapeHTML(content.comment_placeholder)}</span><textarea name="comment" rows="3" maxlength="10000" placeholder="${escapeHTML(content.comment_placeholder)}"></textarea></label>`
    : ''
  const controllerNotice = controllerProfile &&
    content.privacy_purpose_summary?.trim() &&
    content.privacy_lawful_basis_summary?.trim() &&
    content.privacy_retention_summary?.trim()
    ? `<details class="controller-notice">
         <summary data-copy="controller-summary">${escapeHTML(localization.system.controllerNotice.replace('{controller}', controllerProfile.name))}</summary>
         <p data-copy="privacy-purpose">${escapeHTML(content.privacy_purpose_summary ?? '')}</p>
         <p data-copy="privacy-lawful-basis">${escapeHTML(localization.system.lawfulBasis)}: ${escapeHTML(content.privacy_lawful_basis_summary ?? '')}</p>
         <p data-copy="privacy-retention">${escapeHTML(localization.system.retention)}: ${escapeHTML(content.privacy_retention_summary ?? '')}</p>
         <p class="controller-links">
           <a href="mailto:${escapeHTML(controllerProfile.privacy_contact_email)}"><span data-copy="privacy-contact">${escapeHTML(localization.system.privacyContact)}</span></a>
           <a href="mailto:${escapeHTML(controllerProfile.privacy_rights_email)}"><span data-copy="rights-contact">${escapeHTML(localization.system.rightsContact)}</span></a>
           <a data-copy="privacy-link" href="${escapeHTML(controllerProfile.privacy_policy_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(localization.system.privacyPolicy)}</a>
         </p>
       </details>`
    : ''
  const contact = structured && rule.collect_contact
    ? `<label class="consent"><input type="checkbox" name="notify_reporter"><span data-copy="consent-label">${escapeHTML(content.consent_label)}</span></label>
       <div class="contact-detail" data-contact hidden>
         <label><span data-copy="contact-label">${escapeHTML(content.contact_label)}</span><input type="email" name="reporter_email" maxlength="320" autocomplete="email" placeholder="${escapeHTML(localization.system.emailPlaceholder)}"></label>
         <p class="consent-notice"><span data-copy="consent-notice">${escapeHTML(localization.system.consentNotice)}</span></p>
       </div>`
    : ''
  const aiDisclosure = rule.ai_processing?.enabled
    ? `<p class="ai-disclosure" role="note"><span data-copy="ai-disclosure">${escapeHTML(aiDisclosureCopy(rule, localization))}</span>${rule.ai_processing.privacy_policy_url
      ? ` <a data-copy="ai-privacy-link" href="${escapeHTML(rule.ai_processing.privacy_policy_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(localization.system.privacyPolicy)}</a>`
      : ''}</p>`
    : ''
  return `${launcher}<section class="panel ${presentation}" data-panel aria-label="${escapeHTML(localization.system.formLabel)}"${panelHidden}>
    <header><div>${logoURL
      ? `<img class="brand" src="${escapeHTML(logoURL)}" alt="" loading="lazy" referrerpolicy="no-referrer">`
      : '<span class="eyebrow">FIDIBEKI</span>'}<h2 data-copy="prompt">${escapeHTML(content.prompt)}</h2></div>
      <button class="icon" type="button" data-close aria-label="${escapeHTML(localization.system.close)}" title="${escapeHTML(localization.system.close)}">&#215;</button></header>
    <form novalidate>
      <div data-optional class="optional"></div>
      ${legacyText}${comment}${controllerNotice}${contact}${aiDisclosure}
      <label class="honeypot" aria-hidden="true">Website<input name="company_website" tabindex="-1" autocomplete="off"></label>
      <footer><span data-message aria-live="polite"></span><button class="primary" type="submit" data-submit>${escapeHTML(content.submit_label)}</button></footer>
    </form>
  </section>`
}

function aiDisclosureCopy(
  rule: WidgetRule,
  localization: ResolvedWidgetLocalization,
): string {
  const categories = (rule.ai_processing?.input_categories ?? [])
    .map(category => localization.system.aiInputNames[category])
    .filter(Boolean)
  if (categories.length === 0) return localization.system.aiDisclosure
  return `${localization.system.aiDisclosure} ${localization.system.aiScope.replace(
    '{categories}',
    categories.join(', '),
  )}`
}

function styles(
  accent: string,
  accentForeground: string,
  side: 'left' | 'right',
  theme: WidgetRule['theme'],
  surface?: string,
  text?: string,
): string {
  const dark = theme === 'dark'
  const automatic = theme === 'auto' && !surface && !text
  return `:host{--fb-accent:${accent};--fb-on-accent:${accentForeground};--fb-bg:${surface ?? (dark ? '#171918' : '#fff')};--fb-fg:${text ?? (dark ? '#f5f7f6' : '#171a19')};--fb-muted:${dark ? '#aeb8b4' : '#63706b'};--fb-line:${dark ? '#3b413e' : '#d9dfdc'};all:initial;display:block;font-family:"Avenir Next","Segoe UI",sans-serif;color:var(--fb-fg)}
  [hidden]{display:none!important}.launcher{position:fixed;z-index:2147483000;bottom:20px;${side}:20px;min-height:44px;padding:0 16px;border:1px solid var(--fb-accent);border-radius:6px;background:var(--fb-accent);color:var(--fb-on-accent);font:700 13px/1 sans-serif;letter-spacing:0;box-shadow:0 10px 28px rgba(0,0,0,.18);cursor:pointer}
  .panel{position:fixed;z-index:2147483000;bottom:20px;${side}:20px;width:min(368px,calc(100vw - 32px));max-height:calc(100dvh - 40px);display:flex;flex-direction:column;box-sizing:border-box;background:var(--fb-bg);color:var(--fb-fg);border:1px solid var(--fb-line);border-top:4px solid var(--fb-accent);border-radius:6px;box-shadow:0 18px 50px rgba(0,0,0,.2);overflow:hidden}
  .panel.inline{position:relative;inset:auto;width:100%;max-width:720px;box-shadow:none}.panel.reaction{width:min(420px,calc(100vw - 32px))}
  header{display:flex;flex:none;align-items:flex-start;justify-content:space-between;gap:16px;padding:20px 20px 14px}h2{font:700 18px/1.3 "Avenir Next","Segoe UI",sans-serif;margin:3px 0 0;letter-spacing:0}.eyebrow{font:700 10px/1 sans-serif;color:var(--fb-accent);letter-spacing:0}.brand{display:block;width:auto;max-width:132px;height:24px;object-fit:contain;object-position:left center;margin-bottom:7px}form{min-height:0;overflow-y:auto;padding:0 20px 20px}.icon{width:32px;height:32px;display:grid;place-items:center;border:0;background:transparent;color:var(--fb-muted);font-size:24px;line-height:1;cursor:pointer}.icon:hover{color:var(--fb-fg);background:color-mix(in srgb,var(--fb-fg) 7%,transparent)}.icon:disabled{opacity:.5;cursor:wait}label>span,legend{display:block;margin:14px 0 7px;font:650 12px/1.2 sans-serif;color:var(--fb-muted)}textarea,input[type=email],select{box-sizing:border-box;width:100%;padding:11px 12px;border:1px solid var(--fb-line);border-radius:5px;background:var(--fb-bg);color:var(--fb-fg);font:400 14px/1.45 "Avenir Next","Segoe UI",sans-serif;letter-spacing:0;outline:none}textarea{resize:vertical;min-height:92px}textarea:focus,input[type=email]:focus,select:focus{border-color:var(--fb-accent);box-shadow:0 0 0 2px color-mix(in srgb,var(--fb-accent) 22%,transparent)}.controller-notice{margin:12px 0 0;padding:10px 0;border-top:1px solid var(--fb-line);border-bottom:1px solid var(--fb-line);font:400 11px/1.45 sans-serif;color:var(--fb-muted)}.controller-notice summary{cursor:pointer;font-weight:650;color:var(--fb-fg)}.controller-notice p{margin:7px 0 0}.controller-links{display:flex;flex-wrap:wrap;gap:5px 12px}.controller-links a,.consent-notice a,.ai-disclosure a{color:var(--fb-accent)}.consent{display:flex;align-items:flex-start;gap:9px;margin-top:12px}.consent input{margin:2px 0 0}.consent span{margin:0}.consent-notice,.ai-disclosure{margin:6px 0 0;font:400 11px/1.4 sans-serif;color:var(--fb-muted)}.ai-disclosure{padding-top:9px;border-top:1px solid var(--fb-line)}.optional{display:grid;gap:10px}.optional fieldset{border:0;padding:0;margin:0;min-width:0}.scale,.choices{display:flex;flex-wrap:wrap;gap:6px}.choice{min-width:38px;height:38px;padding:0 10px;border:1px solid var(--fb-line);border-radius:5px;background:transparent;color:var(--fb-muted);font:650 13px/1 sans-serif;cursor:pointer}.scale .choice{width:38px;padding:0;font-size:21px}.choice[aria-pressed=true]{border-color:var(--fb-accent);background:color-mix(in srgb,var(--fb-accent) 10%,transparent);color:var(--fb-accent)}[data-invalid=true]{outline:2px solid #b42318;outline-offset:3px}.primary,.secondary{min-height:38px;border-radius:5px;font:700 13px/1 sans-serif;letter-spacing:0;cursor:pointer}.primary{border:1px solid var(--fb-accent);background:var(--fb-accent);color:var(--fb-on-accent);padding:0 15px}.primary:disabled{opacity:.6;cursor:wait}.secondary{width:100%;border:1px solid var(--fb-line);background:transparent;color:var(--fb-fg);padding:0 12px}.secondary:hover{border-color:var(--fb-accent)}footer{display:flex;align-items:center;justify-content:flex-end;gap:12px;margin-top:16px}footer span{flex:1;color:var(--fb-muted);font:500 11px/1.3 sans-serif}.honeypot{position:absolute!important;left:-10000px!important;width:1px!important;height:1px!important;overflow:hidden!important}.success{display:grid;gap:6px;padding:22px 0 8px;text-align:center}.success strong{font:700 18px/1.2 sans-serif}.success span{font:400 13px/1.4 sans-serif;color:var(--fb-muted)}
  ${automatic ? '@media(prefers-color-scheme:dark){:host{--fb-bg:#171918;--fb-fg:#f5f7f6;--fb-muted:#aeb8b4;--fb-line:#3b413e}}' : ''}
  @media(max-width:480px){.panel:not(.inline){bottom:12px;left:12px;right:12px;width:auto}.launcher{bottom:12px;left:auto;right:12px}}
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
