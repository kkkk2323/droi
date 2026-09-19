function toHex(byte: number): string {
  return byte.toString(16).padStart(2, '0')
}

export function uuidFromRandomBytes(input: Uint8Array): string {
  const bytes = new Uint8Array(16)
  bytes.set(input.subarray(0, 16))

  // RFC 4122 v4
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80

  const hex = Array.from(bytes, toHex).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function uuidv4(): string {
  try {
    const c = (globalThis as any)?.crypto as Crypto | undefined
    if (typeof (c as any)?.randomUUID === 'function') return (c as any).randomUUID()
    if (typeof c?.getRandomValues === 'function') {
      const bytes = new Uint8Array(16)
      c.getRandomValues(bytes)
      return uuidFromRandomBytes(bytes)
    }
  } catch {
    // ignore
  }

  // Last-resort fallback (not cryptographically secure).
  const bytes = new Uint8Array(16)
  for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  return uuidFromRandomBytes(bytes)
}
