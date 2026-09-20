import { describe, expect, test } from 'vitest'
import { parseRoute, routeHash } from './use-hash-route'

describe('hash routes', () => {
  test('round-trip every route', () => {
    for (const route of [
      { name: 'home' } as const,
      { name: 'new' } as const,
      { name: 'new', workspace: '/Users/dev/acme web?&x' } as const,
      { name: 'settings' } as const,
      { name: 'session', sessionId: 'abc/def' } as const,
    ]) {
      expect(parseRoute(routeHash(route))).toEqual(route)
    }
  })

  test('unknown fragments are home', () => {
    expect(parseRoute('')).toEqual({ name: 'home' })
    expect(parseRoute('#pair=x')).toEqual({ name: 'home' })
  })
})
