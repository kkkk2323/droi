import { describe, expect, test } from 'vitest'
import { daysLeft, latin1, profileExpiry } from './signature'

const PROFILE = `0\x82\x0b\x01\x06\t*\x86H<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>CreationDate</key>
  <date>2026-09-23T04:10:00Z</date>
  <key>ExpirationDate</key>
  <date>2026-09-30T04:10:00Z</date>
</dict></plist>\x00\xa0\x82`

describe('signature', () => {
  test('reads the expiry out of a provisioning profile', () => {
    const bytes = Uint8Array.from(PROFILE, (c) => c.charCodeAt(0))
    expect(profileExpiry(latin1(bytes))).toEqual(new Date('2026-09-30T04:10:00Z'))
  })

  test('no expiry in the text is null', () => {
    expect(profileExpiry('<plist></plist>')).toBeNull()
  })

  test('days left rounds a part-day up and stops at zero', () => {
    const expiry = new Date('2026-09-30T04:10:00Z')
    expect(daysLeft(expiry, new Date('2026-09-23T04:10:00Z'))).toBe(7)
    expect(daysLeft(expiry, new Date('2026-09-29T20:00:00Z'))).toBe(1)
    expect(daysLeft(expiry, new Date('2026-10-01T00:00:00Z'))).toBe(0)
  })
})
