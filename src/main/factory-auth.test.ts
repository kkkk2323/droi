import { describe, expect, test } from 'vitest'
import { createFactoryAuth, type LoginState } from './factory-auth'

function jwt(claims: Record<string, unknown>): string {
  const b64 = (o: unknown) => Buffer.from(JSON.stringify(o)).toString('base64url')
  return `${b64({ alg: 'none' })}.${b64(claims)}.sig`
}

/** In-memory WorkOS: device authorization, polling and refresh. */
function fakeWorkos(options: { approveAfterPolls: number; now: () => number }) {
  let polls = 0
  let refreshes = 0
  const calls: string[] = []
  const token = (ttlSec: number) =>
    jwt({ sub: 'user_1', org_id: 'org_1', exp: Math.floor(options.now() / 1000) + ttlSec })
  const fetchImpl: typeof fetch = async (input, init) => {
    const url = String(input)
    const body = new URLSearchParams(String(init?.body))
    calls.push(`${url.split('/').pop()} ${body.get('grant_type') ?? ''}`.trim())
    if (url.endsWith('/authorize/device')) {
      return Response.json({
        device_code: 'dev_1',
        user_code: 'ABCD-1234',
        verification_uri: 'https://auth.example/device',
        verification_uri_complete: 'https://auth.example/device?code=ABCD-1234',
        expires_in: 300,
        interval: 0,
      })
    }
    if (url.endsWith('/authenticate') && body.get('grant_type')?.includes('device_code')) {
      polls += 1
      if (polls < options.approveAfterPolls)
        return Response.json({ error: 'authorization_pending' }, { status: 400 })
      return Response.json({ access_token: token(120), refresh_token: 'rt_1' })
    }
    if (url.endsWith('/authenticate') && body.get('grant_type') === 'refresh_token') {
      refreshes += 1
      if (body.get('refresh_token') === 'rt_revoked')
        return Response.json({ error: 'invalid_grant' }, { status: 400 })
      return Response.json({ access_token: token(120), refresh_token: `rt_${refreshes + 1}` })
    }
    if (url.endsWith('/api/cli/whoami')) {
      return Response.json({ userId: 'user_01FACTORY', orgId: 'firestoreOrg' })
    }
    return new Response('not found', { status: 404 })
  }
  return {
    fetchImpl,
    calls,
    get refreshes() {
      return refreshes
    },
  }
}

function memoryStore(initial: string | null = null) {
  let value = initial
  return {
    load: () => value,
    save: (next: string | null) => void (value = next),
    get value() {
      return value
    },
  }
}

describe('factory auth', () => {
  test('device flow: pending with a code, then signed in with Factory ids', async () => {
    let clock = 1_700_000_000_000
    const workos = fakeWorkos({ approveAfterPolls: 3, now: () => clock })
    const store = memoryStore()
    const auth = createFactoryAuth({
      ...store,
      fetch: workos.fetchImpl,
      factoryApiBaseUrl: 'https://api.example',
      now: () => clock,
      sleep: async () => void (clock += 1000),
    })
    const states: LoginState[] = []
    auth.on('change', (s) => states.push(s))

    const pending = await auth.signIn()
    expect(pending.userCode).toBe('ABCD-1234')
    expect(auth.state.status).toBe('pending')

    await new Promise<void>((resolve) =>
      auth.on('change', (s) => s.status === 'signed-in' && resolve()),
    )
    expect(auth.state).toEqual({
      status: 'signed-in',
      account: { userId: 'user_01FACTORY', orgId: 'firestoreOrg', email: null },
    })
    expect(store.value).toContain('rt_1')
    expect(await auth.getAccessToken()).toMatch(/^eyJ/)
    expect(workos.refreshes).toBe(0)
  })

  test('refreshes a token that is about to expire and stores the new refresh token', async () => {
    let clock = 1_700_000_000_000
    const workos = fakeWorkos({ approveAfterPolls: 1, now: () => clock })
    const stored = JSON.stringify({
      accessToken: jwt({ sub: 'u', exp: Math.floor(clock / 1000) + 30 }),
      refreshToken: 'rt_old',
      account: { userId: 'u', orgId: null, email: null },
    })
    const store = memoryStore(stored)
    const auth = createFactoryAuth({ ...store, fetch: workos.fetchImpl, now: () => clock })
    expect(auth.state.status).toBe('signed-in')

    const [a, b] = await Promise.all([auth.getAccessToken(), auth.getAccessToken()])
    expect(a).toBe(b)
    expect(workos.refreshes).toBe(1)
    expect(store.value).toContain('rt_2')
  })

  test('a revoked refresh token signs the user out with an explanation', async () => {
    const clock = 1_700_000_000_000
    const workos = fakeWorkos({ approveAfterPolls: 1, now: () => clock })
    const store = memoryStore(
      JSON.stringify({
        accessToken: jwt({ sub: 'u', exp: 1 }),
        refreshToken: 'rt_revoked',
        account: { userId: 'u', orgId: null, email: null },
      }),
    )
    const auth = createFactoryAuth({ ...store, fetch: workos.fetchImpl, now: () => clock })
    expect(await auth.getAccessToken()).toBeNull()
    expect(auth.state).toEqual({
      status: 'signed-out',
      error: 'Your Factory session expired. Sign in again.',
    })
    expect(store.value).toBeNull()
  })

  test('sign out clears the stored login', () => {
    const store = memoryStore(
      JSON.stringify({
        accessToken: 'x.y.z',
        refreshToken: 'rt',
        account: { userId: 'u', orgId: null, email: null },
      }),
    )
    const auth = createFactoryAuth({ ...store })
    auth.signOut()
    expect(auth.state.status).toBe('signed-out')
    expect(store.value).toBeNull()
  })
})
