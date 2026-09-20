// "Sign in with Factory" for the Desktop Shell. This is the same OAuth device
// flow the `droid` CLI uses (WorkOS User Management, the CLI's client id), so
// the resulting access token is exactly what the Daemon accepts as `token` in
// `daemon.authenticate`. Tokens live in the encrypted settings file; a Client
// only ever sees the Gateway placeholder.
import { EventEmitter } from 'node:events'

/** WorkOS client of the production `droid` CLI; the Daemon trusts its JWTs. */
export const FACTORY_CLI_WORKOS_CLIENT_ID = 'client_01HNM792M5G5G1A2THWPXKFMXB'
export const WORKOS_BASE_URL = 'https://api.workos.com/user_management'
const DEVICE_GRANT = 'urn:ietf:params:oauth:grant-type:device_code'
/** Refresh this long before the access token expires. */
const REFRESH_MARGIN_MS = 60_000
const REFRESH_ATTEMPTS = 3

export interface FactoryAccount {
  userId: string
  orgId: string | null
  email: string | null
}

export interface LoginPending {
  userCode: string
  verificationUri: string
  verificationUriComplete: string
  /** Unix ms when the code stops working. */
  expiresAt: number
}

export type LoginState =
  | { status: 'signed-out'; error: string | null }
  | { status: 'pending'; pending: LoginPending }
  | { status: 'signed-in'; account: FactoryAccount }

interface StoredLogin {
  accessToken: string
  refreshToken: string
  account: FactoryAccount
}

export interface FactoryAuthOptions {
  load(): string | null
  save(serialized: string | null): void
  fetch?: typeof fetch
  workosBaseUrl?: string
  clientId?: string
  /** Factory API for `whoami` (Factory's own user/org ids); null skips it. */
  factoryApiBaseUrl?: string | null
  now?: () => number
  sleep?: (ms: number) => Promise<void>
}

export interface FactoryAuth extends EventEmitter<{ change: [LoginState] }> {
  readonly state: LoginState
  /** Starts the device flow; resolves once the user has a code to enter. */
  signIn(): Promise<LoginPending>
  cancelSignIn(): void
  signOut(): void
  /** A fresh access token, refreshed when close to expiry; null when signed out. */
  getAccessToken(): Promise<string | null>
}

export function createFactoryAuth(options: FactoryAuthOptions): FactoryAuth {
  const fetchImpl = options.fetch ?? fetch
  const workos = options.workosBaseUrl ?? WORKOS_BASE_URL
  const clientId = options.clientId ?? FACTORY_CLI_WORKOS_CLIENT_ID
  const now = options.now ?? Date.now
  const sleep = options.sleep ?? ((ms) => new Promise((r) => setTimeout(r, ms)))

  let login = parseStored(options.load())
  let state: LoginState = login
    ? { status: 'signed-in', account: login.account }
    : { status: 'signed-out', error: null }
  let pollAbort: AbortController | null = null
  let refreshing: Promise<string | null> | null = null

  const emitter = new EventEmitter<{ change: [LoginState] }>() as FactoryAuth
  const setState = (next: LoginState) => {
    state = next
    emitter.emit('change', next)
  }
  const persist = (next: StoredLogin | null) => {
    login = next
    options.save(next ? JSON.stringify(next) : null)
  }

  const tokenRequest = async (body: Record<string, string>): Promise<Response> =>
    fetchImpl(`${workos}/authenticate`, {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ ...body, client_id: clientId }),
    })

  // The JWT carries the WorkOS ids; the Daemon compares Factory's own
  // (Firestore) ids, which `whoami` returns, so prefer those when reachable.
  const describeAccount = async (accessToken: string): Promise<FactoryAccount> => {
    const claims = decodeJwt(accessToken)
    const account: FactoryAccount = {
      userId: typeof claims['sub'] === 'string' ? claims['sub'] : 'unknown',
      orgId: typeof claims['org_id'] === 'string' ? claims['org_id'] : null,
      email: typeof claims['email'] === 'string' ? claims['email'] : null,
    }
    if (!options.factoryApiBaseUrl) return account
    try {
      const response = await fetchImpl(`${options.factoryApiBaseUrl}/api/cli/whoami`, {
        headers: { authorization: `Bearer ${accessToken}` },
      })
      if (response.ok) {
        const body = (await response.json()) as { userId?: unknown; orgId?: unknown }
        if (typeof body.userId === 'string') account.userId = body.userId
        if (typeof body.orgId === 'string') account.orgId = body.orgId
      }
    } catch {
      // Cosmetic: the token already proves who this is.
    }
    return account
  }

  const poll = async (deviceCode: string, intervalSec: number, expiresAt: number) => {
    const abort = new AbortController()
    pollAbort = abort
    let interval = intervalSec
    try {
      while (!abort.signal.aborted && now() < expiresAt) {
        await sleep(interval * 1000)
        if (abort.signal.aborted) return
        const response = await tokenRequest({ grant_type: DEVICE_GRANT, device_code: deviceCode })
        const text = await response.text()
        if (response.ok) {
          const tokens = JSON.parse(text) as { access_token: string; refresh_token: string }
          const account = await describeAccount(tokens.access_token)
          persist({
            accessToken: tokens.access_token,
            refreshToken: tokens.refresh_token,
            account,
          })
          setState({ status: 'signed-in', account })
          return
        }
        const error = parseError(text)
        if (error === 'authorization_pending') continue
        if (error === 'slow_down') {
          interval += 1
          continue
        }
        throw new Error(
          error === 'access_denied'
            ? 'Sign-in was denied in the browser.'
            : error === 'expired_token'
              ? 'The sign-in code expired.'
              : `Sign-in failed (${error ?? response.status}).`,
        )
      }
      if (!abort.signal.aborted) throw new Error('The sign-in code expired.')
    } catch (error) {
      if (abort.signal.aborted) return
      setState({
        status: 'signed-out',
        error: error instanceof Error ? error.message : String(error),
      })
    } finally {
      if (pollAbort === abort) pollAbort = null
    }
  }

  const refresh = async (current: StoredLogin): Promise<string | null> => {
    for (let attempt = 1; attempt <= REFRESH_ATTEMPTS; attempt += 1) {
      let response: Response
      try {
        response = await tokenRequest({
          grant_type: 'refresh_token',
          refresh_token: current.refreshToken,
          ...(current.account.orgId ? { organization_id: current.account.orgId } : {}),
        })
      } catch {
        if (attempt < REFRESH_ATTEMPTS) await sleep(500 * attempt)
        continue
      }
      if (response.ok) {
        const tokens = (await response.json()) as { access_token: string; refresh_token: string }
        persist({
          ...current,
          accessToken: tokens.access_token,
          refreshToken: tokens.refresh_token,
        })
        return tokens.access_token
      }
      // 4xx other than 429: the refresh token is gone for good; the user must sign in again.
      if (response.status >= 400 && response.status < 500 && response.status !== 429) {
        persist(null)
        setState({ status: 'signed-out', error: 'Your Factory session expired. Sign in again.' })
        return null
      }
      if (attempt < REFRESH_ATTEMPTS) await sleep(500 * attempt)
    }
    return null
  }

  Object.defineProperty(emitter, 'state', { get: () => state })
  emitter.signIn = async () => {
    pollAbort?.abort()
    const response = await fetchImpl(`${workos}/authorize/device`, {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ client_id: clientId }),
    })
    if (!response.ok) throw new Error(`Could not start sign-in (${response.status}).`)
    const body = (await response.json()) as {
      device_code: string
      user_code: string
      verification_uri: string
      verification_uri_complete: string
      expires_in: number
      interval: number
    }
    const pending: LoginPending = {
      userCode: body.user_code,
      verificationUri: body.verification_uri,
      verificationUriComplete: body.verification_uri_complete,
      expiresAt: now() + body.expires_in * 1000,
    }
    setState({ status: 'pending', pending })
    void poll(body.device_code, body.interval, pending.expiresAt)
    return pending
  }
  emitter.cancelSignIn = () => {
    if (!pollAbort) return
    pollAbort.abort()
    pollAbort = null
    setState(
      login
        ? { status: 'signed-in', account: login.account }
        : { status: 'signed-out', error: null },
    )
  }
  emitter.signOut = () => {
    pollAbort?.abort()
    pollAbort = null
    persist(null)
    setState({ status: 'signed-out', error: null })
  }
  emitter.getAccessToken = async () => {
    const current = login
    if (!current) return null
    const expiresAt = jwtExpiryMs(current.accessToken)
    if (expiresAt !== null && expiresAt - now() > REFRESH_MARGIN_MS) return current.accessToken
    refreshing ??= refresh(current).finally(() => {
      refreshing = null
    })
    return refreshing
  }
  return emitter
}

function parseStored(serialized: string | null): StoredLogin | null {
  if (!serialized) return null
  try {
    const parsed = JSON.parse(serialized) as Partial<StoredLogin>
    if (
      typeof parsed.accessToken !== 'string' ||
      typeof parsed.refreshToken !== 'string' ||
      typeof parsed.account?.userId !== 'string'
    )
      return null
    return {
      accessToken: parsed.accessToken,
      refreshToken: parsed.refreshToken,
      account: {
        userId: parsed.account.userId,
        orgId: typeof parsed.account.orgId === 'string' ? parsed.account.orgId : null,
        email: typeof parsed.account.email === 'string' ? parsed.account.email : null,
      },
    }
  } catch {
    return null
  }
}

function parseError(text: string): string | null {
  try {
    const parsed = JSON.parse(text) as { error?: unknown }
    return typeof parsed.error === 'string' ? parsed.error : null
  } catch {
    return null
  }
}

export function decodeJwt(token: string): Record<string, unknown> {
  const payload = token.split('.')[1]
  if (!payload) return {}
  try {
    return JSON.parse(Buffer.from(payload, 'base64url').toString()) as Record<string, unknown>
  } catch {
    return {}
  }
}

export function jwtExpiryMs(token: string): number | null {
  const exp = decodeJwt(token)['exp']
  return typeof exp === 'number' ? exp * 1000 : null
}
