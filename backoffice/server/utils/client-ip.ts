import { isIP } from 'node:net'

type ParsedIP = { bits: 32 | 128; value: bigint; normalized: string }

const zero = BigInt(0)
const eight = BigInt(8)
const sixteen = BigInt(16)
const mask16 = BigInt(0xffff)

function parseIP(value: string): ParsedIP | undefined {
  const normalized = value.trim().replace(/^\[|\]$/g, '')
  const version = isIP(normalized)
  if (version === 4) {
    const parts = normalized.split('.').map(Number)
    const integer = parts.reduce((current, part) => (current << eight) + BigInt(part), zero)
    return { bits: 32, value: integer, normalized }
  }
  if (version !== 6) return undefined

  const pieces = normalized.split('::', 2)
  let left = pieces[0] ?? ''
  let right = pieces[1]
  // Node can report IPv4-mapped IPv6 peers (for example ::ffff:127.0.0.1).
  // Expand the dotted tail before interpreting each 16-bit group.
  if (normalized.includes('.')) {
    const dotted = normalized.slice(normalized.lastIndexOf(':') + 1)
    const mapped = parseIP(dotted)
    if (!mapped || mapped.bits !== 32) return undefined
    const expanded = `${(mapped.value >> sixteen).toString(16)}:${(mapped.value & mask16).toString(16)}`
    if (right?.includes('.')) right = right.replace(dotted, expanded)
    else left = left.replace(dotted, expanded)
  }
  const leftParts = left ? left.split(':') : []
  const rightParts = right ? right.split(':') : []
  const parts = [...leftParts, ...Array(8 - leftParts.length - rightParts.length).fill('0'), ...rightParts]
  const integer = parts.reduce((current, part) => (current << sixteen) + BigInt(`0x${part || '0'}`), zero)
  return { bits: 128, value: integer, normalized }
}

function isWithinCIDR(address: ParsedIP, cidr: string): boolean {
  const [network, prefixText, ...rest] = cidr.trim().split('/')
  if (rest.length || !prefixText) return false
  const parsedNetwork = parseIP(network ?? '')
  const prefix = Number(prefixText)
  if (!parsedNetwork || parsedNetwork.bits !== address.bits || !Number.isInteger(prefix) || prefix < 0 || prefix > address.bits) return false
  if (prefix === 0) return true
  const mask = ((BigInt(1) << BigInt(prefix)) - BigInt(1)) << BigInt(address.bits - prefix)
  return (parsedNetwork.value & mask) === (address.value & mask)
}

function isTrusted(address: ParsedIP, cidrs: string[]): boolean {
  return cidrs.some((cidr) => isWithinCIDR(address, cidr))
}

// trustedClientIP uses X-Forwarded-For only when the direct peer is in a
// deployment-configured proxy network. It then walks the chain from the
// closest hop outward, returning the first untrusted client address.
export function trustedClientIP(remoteAddress: string | undefined, forwardedFor: string | undefined, configuredCIDRs: string): string {
  const peer = parseIP(remoteAddress || '')
  if (!peer) return ''
  const trustedCIDRs = configuredCIDRs.split(',').map((cidr) => cidr.trim()).filter(Boolean)
  if (!isTrusted(peer, trustedCIDRs)) return peer.normalized

  let address = peer
  for (const candidate of (forwardedFor || '').split(',').reverse()) {
    const parsed = parseIP(candidate)
    if (!parsed) continue
    address = parsed
    if (!isTrusted(parsed, trustedCIDRs)) break
  }
  return address.normalized
}
