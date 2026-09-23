import { describe, expect, test } from 'vitest'
import { resolveClientConfig, type ClientEnvironment } from './client-config'

function environment(overrides: Partial<ClientEnvironment> = {}): ClientEnvironment & {
  stored: Map<string, string>
  replaced: string[]
} {
  const stored = new Map<string, string>()
  const replaced: string[] = []
  return {
    stored,
    replaced,
    origin: 'http://192.168.1.10:4567',
    hash: '',
    droiShell: undefined,
    storage: {
      getItem: (key) => stored.get(key) ?? null,
      setItem: (key, value) => void stored.set(key, value),
      removeItem: (key) => void stored.delete(key),
    },
    replaceUrl: (url) => void replaced.push(url),
    ...overrides,
  }
}

describe('resolveClientConfig', () => {
  test('Local Client takes everything from the Desktop Shell preload', () => {
    const env = environment({
      droiShell: {
        gatewayUrl: 'http://127.0.0.1:5000',
        pairingToken: 'local-token',
        platform: 'darwin',
        settings: {} as never,
        openIn: {} as never,
        alerts: {} as never,
      },
      hash: '#pair=ignored',
    })
    expect(resolveClientConfig(env)).toEqual({
      kind: 'local',
      gatewayUrl: 'http://127.0.0.1:5000',
      pairingToken: 'local-token',
    })
    expect(env.replaced).toEqual([])
  })

  test('Remote Client stores the token from the fragment and strips it from the URL', () => {
    const env = environment({ hash: '#pair=abc123' })
    expect(resolveClientConfig(env)).toEqual({
      kind: 'remote',
      gatewayUrl: 'http://192.168.1.10:4567',
      pairingToken: 'abc123',
    })
    expect(env.stored.get('droi.pairingToken')).toBe('abc123')
    expect(env.replaced).toEqual(['http://192.168.1.10:4567/'])
  })

  test('Remote Client reuses a stored token when the fragment is empty', () => {
    const env = environment()
    env.stored.set('droi.pairingToken', 'stored')
    expect(resolveClientConfig(env).pairingToken).toBe('stored')
    expect(env.replaced).toEqual([])
  })

  test('a fragment may also point at another Gateway (dev and tests)', () => {
    const env = environment({ hash: '#pair=t&gateway=http://127.0.0.1:9999' })
    expect(resolveClientConfig(env)).toEqual({
      kind: 'remote',
      gatewayUrl: 'http://127.0.0.1:9999',
      pairingToken: 't',
    })
    const again = environment({ storage: env.storage })
    expect(resolveClientConfig(again).gatewayUrl).toBe('http://127.0.0.1:9999')
  })

  test('no token anywhere yields null', () => {
    expect(resolveClientConfig(environment()).pairingToken).toBeNull()
  })
})
