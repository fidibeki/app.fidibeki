import { collectMetadata, detectBrowser, detectDevice, detectOS } from '../../src/metadata'

describe('metadata collection', () => {
  it.each([
    ['Mozilla/5.0 Chrome/120.0', 'Chrome'],
    ['Mozilla/5.0 Edg/120.0', 'Edge'],
    ['Mozilla/5.0 Firefox/120.0', 'Firefox'],
    ['Mozilla/5.0 Version/17.0 Safari/605.1', 'Safari'],
    ['unknown', 'Other'],
  ])('detects browser for %s', (userAgent, expected) => expect(detectBrowser(userAgent)).toBe(expected))

  it.each([
    ['Windows NT 10.0', 'Windows'], ['Android 15', 'Android'], ['iPhone OS 18', 'iOS'],
    ['Mac OS X 14', 'macOS'], ['Linux x86_64', 'Linux'], ['unknown', 'Other'],
  ])('detects OS for %s', (userAgent, expected) => expect(detectOS(userAgent)).toBe(expected))

	it.each([[390, 'mobile'], [768, 'tablet'], [1025, 'desktop']] as const)(
		'detects %s px as %s',
		(width, expected) => expect(detectDevice(width)).toBe(expected),
	)

  it('collects page and viewport fields', () => {
    const metadata = collectMetadata(window, document, { userAgent: 'Firefox/120 Linux' } as Navigator)
    expect(metadata).toMatchObject({ browser: 'Firefox', os: 'Linux' })
    expect(metadata.url).toBe(window.location.href)
    expect(metadata.viewport_width).toBe(window.innerWidth)
  })
})
