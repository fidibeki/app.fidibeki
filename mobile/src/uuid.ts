export type RandomBytes = (target: Uint8Array) => Uint8Array

function defaultRandomBytes(target: Uint8Array): Uint8Array {
  const cryptoObject = globalThis.crypto
  if (cryptoObject?.getRandomValues) {
    cryptoObject.getRandomValues(target as Uint8Array<ArrayBuffer>)
    return target
  }
  // The UUID is an idempotency key, not an authenticator. React Native does not
  // expose Web Crypto on every supported runtime, so retain 122 random UUID bits
  // without deriving or persisting a device identifier.
  for (let index = 0; index < target.length; index += 1) {
    target[index] = Math.floor(Math.random() * 256)
  }
  return target
}

export function createUUID(randomBytes: RandomBytes = defaultRandomBytes): string {
  const bytes = randomBytes(new Uint8Array(16))
  bytes[6] = (bytes[6] ?? 0) & 0x0f | 0x40
  bytes[8] = (bytes[8] ?? 0) & 0x3f | 0x80
  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20),
  ].join('-')
}
