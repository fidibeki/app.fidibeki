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

  it('collects only the supplied safe page and coarse browser context', () => {
    Object.defineProperty(document, 'referrer', {
      configurable: true,
      value: 'https://external.example/search?q=jane.private@example.test',
    })
    const metadata = collectMetadata(
      'https://example.com/accounts',
      window,
      { userAgent: 'Firefox/120 Linux account-48291' } as Navigator,
    )
    expect(metadata).toMatchObject({ browser: 'Firefox', os: 'Linux' })
    expect(metadata.url).toBe('https://example.com/accounts')
    expect(metadata.referrer).toBe('')
    expect(metadata.user_agent).toBe('')
    expect(JSON.stringify(metadata)).not.toContain('jane.private')
    expect(JSON.stringify(metadata)).not.toContain('48291')
    expect(metadata.viewport_width).toBe(window.innerWidth)
  })
})
