import type { FeedbackMetadata } from './types'

export function collectMetadata(
  pageURL: string,
  windowObject: Window = window,
  navigatorObject: Navigator = navigator,
): FeedbackMetadata {
  const userAgent = navigatorObject.userAgent
  return {
    url: pageURL,
    referrer: '',
    user_agent: '',
    browser: detectBrowser(userAgent),
    os: detectOS(userAgent),
		device: detectDevice(windowObject.innerWidth),
    viewport_width: windowObject.innerWidth,
    viewport_height: windowObject.innerHeight,
		language: navigatorObject.language || '',
  }
}

export function detectDevice(viewportWidth: number): 'desktop' | 'tablet' | 'mobile' {
	if (viewportWidth <= 767) return 'mobile'
	if (viewportWidth <= 1024) return 'tablet'
	return 'desktop'
}

export function detectBrowser(userAgent: string): string {
  if (/Edg\//.test(userAgent)) return 'Edge'
  if (/OPR\//.test(userAgent)) return 'Opera'
  if (/Chrome\//.test(userAgent)) return 'Chrome'
  if (/Firefox\//.test(userAgent)) return 'Firefox'
  if (/Safari\//.test(userAgent) && /Version\//.test(userAgent)) return 'Safari'
  return 'Other'
}

export function detectOS(userAgent: string): string {
  if (/Windows NT/.test(userAgent)) return 'Windows'
  if (/Android/.test(userAgent)) return 'Android'
  if (/iPhone|iPad|iPod/.test(userAgent)) return 'iOS'
  if (/Mac OS X/.test(userAgent)) return 'macOS'
  if (/Linux/.test(userAgent)) return 'Linux'
  return 'Other'
}
