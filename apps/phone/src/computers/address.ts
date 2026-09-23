// A Gateway address as the user types it: "192.168.1.10:41417",
// "my-mac.tailnet.ts.net:41417" or a full URL. Stored as the origin only.

export function normalizeAddress(text: string): string | null {
  const trimmed = text.trim()
  if (!trimmed || /\s/.test(trimmed)) return null
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`
  try {
    const url = new URL(withScheme)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    // Some URL parsers percent-encode what no host name may contain.
    if (!/^(\[[0-9a-f:.]+\]|[a-z0-9.-]+)$/i.test(url.hostname)) return null
    return url.origin
  } catch {
    return null
  }
}
