import { describe, expect, test } from 'vitest'
import { typeset } from './math'

describe('typeset', () => {
  test('a formula becomes an SVG sized in em, with its depth below the baseline', () => {
    const formula = typeset('\\frac{p_r}{p_o} \\;>\\; \\frac{\\Delta O}{N \\cdot C}', true)
    if (!formula) throw new Error('expected a formula')
    expect(formula.svg).toMatch(/^<svg[^>]*viewBox="[-\d. ]+"/)
    expect(formula.svg).not.toMatch(/<svg[^>]*\b(width|height|style)=/)
    expect(formula.width).toBeGreaterThan(3)
    expect(formula.height).toBeGreaterThan(1.5)
    expect(formula.depth).toBeGreaterThan(0.3)
  })

  test('a letter sits on the baseline', () => {
    const formula = typeset('N', false)
    expect(formula?.depth).toBeCloseTo(0, 1)
    expect(formula?.height).toBeGreaterThan(0.5)
  })

  test('TeX that does not parse gives null', () => {
    expect(typeset('\\frac{a', false)).toBeNull()
    expect(typeset('\\nosuchmacro x', false)).toBeNull()
  })

  test('text in a formula stays', () => {
    expect(typeset('\\text{省下} = N', true)?.svg).toMatch(/省.*下/)
  })
})
