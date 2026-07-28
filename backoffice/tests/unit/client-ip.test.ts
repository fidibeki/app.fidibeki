import { describe, expect, it } from 'vitest'
import { trustedClientIP } from '~/server/utils/client-ip'

describe('trustedClientIP', () => {
  it('ignores a forwarding header from an untrusted direct peer', () => {
    expect(trustedClientIP('198.51.100.5', '203.0.113.9', '10.0.0.0/8')).toBe('198.51.100.5')
  })

  it('walks trusted proxy hops from right to left', () => {
    expect(trustedClientIP('10.0.0.3', '203.0.113.9, 198.51.100.8, 10.0.0.2', '10.0.0.0/8')).toBe('198.51.100.8')
  })

  it('falls back to the trusted peer when forwarding is absent or malformed', () => {
    expect(trustedClientIP('10.0.0.3', 'not-an-ip', '10.0.0.0/8')).toBe('10.0.0.3')
  })

  it('supports IPv6 trusted proxy networks', () => {
    expect(trustedClientIP('2001:db8:1::3', '2001:db8:2::9, 2001:db8:1::2', '2001:db8:1::/64')).toBe('2001:db8:2::9')
  })

  it('handles IPv4-mapped IPv6 socket peers without trusting a header by default', () => {
    expect(trustedClientIP('::ffff:127.0.0.1', '203.0.113.9', '')).toBe('::ffff:127.0.0.1')
  })
})
