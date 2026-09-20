import { describe, expect, it } from 'vitest'
import { FONTS, parseFont } from './font'

describe('parseFont', () => {
  it('accepts every known face and falls back to Geist otherwise', () => {
    for (const f of FONTS) expect(parseFont(f)).toBe(f)
    expect(parseFont('comic-sans')).toBe('geist')
    expect(parseFont('')).toBe('geist')
  })
})
