import { describe, expect, test } from 'vitest'
import { parsePairingInput } from './pairing'

describe('parsePairingInput', () => {
  test('takes the whole link and reads the Gateway from it', () => {
    expect(parsePairingInput(' http://192.168.5.123:41417/#pair=abc-123 ')).toEqual({
      token: 'abc-123',
      gateway: 'http://192.168.5.123:41417',
    })
  })

  test('an explicit gateway in the fragment wins; a bare fragment or token also works', () => {
    expect(parsePairingInput('http://a:1/#pair=t&gateway=http://b:2')).toEqual({
      token: 't',
      gateway: 'http://b:2',
    })
    expect(parsePairingInput('#pair=t')).toEqual({ token: 't', gateway: null })
    expect(parsePairingInput('t0k_en')).toEqual({ token: 't0k_en', gateway: null })
  })

  test('anything else is rejected', () => {
    expect(parsePairingInput('')).toBeNull()
    expect(parsePairingInput('http://a:1/#/settings')).toBeNull()
    expect(parsePairingInput('hello world')).toBeNull()
  })
})
