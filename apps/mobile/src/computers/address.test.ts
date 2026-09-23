import { describe, expect, test } from 'vitest'
import { normalizeAddress } from './address'

describe('normalizeAddress', () => {
  test('adds http and keeps only the origin', () => {
    expect(normalizeAddress(' 192.168.1.10:41417 ')).toBe('http://192.168.1.10:41417')
    expect(normalizeAddress('my-mac.tail1.ts.net:41417/')).toBe('http://my-mac.tail1.ts.net:41417')
    expect(normalizeAddress('https://mac.example.com/#pair=x')).toBe('https://mac.example.com')
  })

  test('refuses what is not an http address', () => {
    expect(normalizeAddress('')).toBeNull()
    expect(normalizeAddress('ftp://mac:21')).toBeNull()
    expect(normalizeAddress('http://')).toBeNull()
    expect(normalizeAddress('not a url at all')).toBeNull()
    expect(normalizeAddress('http://my%20mac:1')).toBeNull()
  })
})
