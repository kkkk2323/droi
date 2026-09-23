// A free Apple account signs the Phone App for seven days. The expiry is in
// the provisioning profile embedded in the app: a signed container holding a
// plain XML property list, so the date can be read without verifying it.

export function profileExpiry(profile: string): Date | null {
  const match = /<key>ExpirationDate<\/key>\s*<date>([^<]+)<\/date>/.exec(profile)
  if (!match) return null
  const date = new Date(match[1]!)
  return Number.isNaN(date.getTime()) ? null : date
}

/** Whole days left, counting a part-day as a day; 0 once it has lapsed. */
export function daysLeft(expiry: Date, now: Date): number {
  const ms = expiry.getTime() - now.getTime()
  return ms <= 0 ? 0 : Math.ceil(ms / 86_400_000)
}

/** The bytes of a binary file as a string, one char per byte, for regex search. */
export function latin1(bytes: Uint8Array): string {
  let text = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    text += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return text
}
