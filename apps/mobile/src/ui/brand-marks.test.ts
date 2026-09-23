/// <reference types="node" />
// Runs in vitest under Node; the app itself has no Node types.
import { readFileSync } from 'node:fs'
import { describe, expect, test } from 'vitest'
import * as marks from './brand-marks'

describe('brand marks', () => {
  test.each(Object.entries(marks))('%s is the web Client’s SVG', (brand, svg) => {
    const web = readFileSync(
      new URL(`../../../desktop/src/renderer/src/assets/providers/${brand}.svg`, import.meta.url),
      'utf8',
    )
    expect(svg).toBe(web.trim())
  })
})
