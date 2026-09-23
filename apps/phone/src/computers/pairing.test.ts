import { describe, expect, test } from 'vitest'
import { PairingError, resolvePairing } from './pairing'

const META = {
  app: 'Droi',
  version: '1.2.0',
  remoteAccess: true,
  name: 'Studio Mac',
  computerId: 'c-1',
}

function fakeFetch(body: unknown, status = 200) {
  const calls: string[] = []
  const impl = (async (input: RequestInfo | URL) => {
    calls.push(String(input))
    return new Response(JSON.stringify(body), { status })
  }) as typeof fetch
  return { impl, calls }
}

describe('resolvePairing', () => {
  test('reads the address and token from the link and the name and id from /meta', async () => {
    const { impl, calls } = fakeFetch(META)
    await expect(
      resolvePairing(' http://192.168.5.123:41417/#pair=abc-123 ', impl),
    ).resolves.toEqual({
      id: 'c-1',
      name: 'Studio Mac',
      address: 'http://192.168.5.123:41417',
      token: 'abc-123',
      version: '1.2.0',
    })
    expect(calls).toEqual(['http://192.168.5.123:41417/meta'])
  })

  test('a bare token or anything else is refused before any request', async () => {
    const { impl, calls } = fakeFetch(META)
    await expect(resolvePairing('abc-123', impl)).rejects.toThrow(/whole pairing link/)
    await expect(resolvePairing('hello world', impl)).rejects.toBeInstanceOf(PairingError)
    expect(calls).toEqual([])
  })

  test('an unreachable Gateway or an old Droi says what to check', async () => {
    const down = (async () => {
      throw new TypeError('Network request failed')
    }) as typeof fetch
    await expect(resolvePairing('http://10.0.0.2:1/#pair=t', down)).rejects.toThrow(
      /Cannot reach http:\/\/10\.0\.0\.2:1.*Remote Access/,
    )
    const old = fakeFetch({ app: 'Droi', version: '1.1.0', remoteAccess: true })
    await expect(resolvePairing('http://10.0.0.2:1/#pair=t', old.impl)).rejects.toThrow(
      /Update Droi/,
    )
  })
})
