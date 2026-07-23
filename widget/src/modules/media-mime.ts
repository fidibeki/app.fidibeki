export type MimeSupportCheck = (mimeType: string) => boolean

// Ordered by preference: codec-qualified variants first so two browsers that both support the
// base container (e.g. "audio/webm") still converge on the same negotiated codec where possible.
export const AUDIO_MIME_CANDIDATES: readonly string[] = [
	'audio/webm;codecs=opus',
	'audio/webm',
	'audio/ogg;codecs=opus',
	'audio/ogg',
	'audio/mp4',
]

export const VIDEO_MIME_CANDIDATES: readonly string[] = [
	'video/webm;codecs=vp9,opus',
	'video/webm;codecs=vp8,opus',
	'video/webm',
	'video/mp4',
]

// Firefox has historically defaulted an unqualified `new MediaRecorder(stream)` audio recording
// to an Ogg/Opus container. Requesting a supported type explicitly (instead of relying on the
// browser's implicit default) is what makes container selection deterministic across browsers.
export function resolveRecorderMimeType(
	candidates: readonly string[],
	isSupported: MimeSupportCheck = nativeMimeTypeSupported,
): string | undefined {
	for (const candidate of candidates) {
		try {
			if (isSupported(candidate)) return candidate
		} catch {
			// A throwing isTypeSupported implementation is treated as unsupported.
		}
	}
	return undefined
}

// The value actually used to label recorded bytes. `recorder.mimeType` reflects what the UA is
// really producing and must always win; the requested candidate is only a fallback for the rare
// case a UA reports an empty mimeType, and a bare kind/webm literal is never used here because
// that is exactly the mislabeling this module exists to prevent.
export function actualChunkMimeType(
	recorder: { mimeType?: string } | null | undefined,
	requestedMimeType: string | undefined,
): string | undefined {
	const actual = recorder?.mimeType?.trim()
	if (actual) return actual
	return requestedMimeType
}

function nativeMimeTypeSupported(mimeType: string): boolean {
	const Recorder = (globalThis as {
		MediaRecorder?: { isTypeSupported?(type: string): boolean }
	}).MediaRecorder
	return typeof Recorder?.isTypeSupported === 'function' && Recorder.isTypeSupported(mimeType)
}
