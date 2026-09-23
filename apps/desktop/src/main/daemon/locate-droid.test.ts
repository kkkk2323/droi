import { describe, expect, test } from 'vitest'
import { locateDroid } from './locate-droid'

const exists = (present: string[]) => (path: string) => present.includes(path)

describe('locateDroid', () => {
  const base = { pathEnv: '/usr/bin:/opt/homebrew/bin', homedir: '/Users/me' }

  test('prefers the settings override when it exists', () => {
    const found = locateDroid({
      ...base,
      override: '/custom/droid',
      exists: exists(['/custom/droid', '/usr/bin/droid']),
    })
    expect(found).toBe('/custom/droid')
  })

  test('ignores an override that does not exist', () => {
    const found = locateDroid({
      ...base,
      override: '/missing/droid',
      exists: exists(['/usr/bin/droid']),
    })
    expect(found).toBe('/usr/bin/droid')
  })

  test('walks PATH in order', () => {
    const found = locateDroid({
      ...base,
      exists: exists(['/opt/homebrew/bin/droid', '/usr/bin/droid']),
    })
    expect(found).toBe('/usr/bin/droid')
  })

  test('falls back to ~/.local/bin/droid', () => {
    const found = locateDroid({ ...base, exists: exists(['/Users/me/.local/bin/droid']) })
    expect(found).toBe('/Users/me/.local/bin/droid')
  })

  test('returns null when nothing is found', () => {
    expect(locateDroid({ ...base, exists: () => false })).toBeNull()
  })
})
