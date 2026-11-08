import { chmodSync, mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import type { AuthProvider } from '@earendil-works/pi-mcp'
import {
  adaptOAuthProvider,
  authorizeMcp,
  McpOAuthAuthorizationRequiredError,
  McpOAuthProvider,
  OAuthCallbackServer,
  type McpOAuthState,
  type McpOAuthStateStore,
} from '@earendil-works/pi-mcp/oauth'
import { info, warn } from '../log.ts'

/** How long a person has to finish signing in. */
const AUTHORIZE_TIMEOUT_MS = 5 * 60_000

/** What `<dataDir>/mcp-auth.json` keeps per server: pi-mcp's state, and the redirect its client was registered with. */
type Stored = { state?: McpOAuthState; redirectUrl?: string }

type Pending = { callback: OAuthCallbackServer }

/** What the authorization a server asked for while connecting needs, kept for the sign-in that follows. */
type Challenge = { scope?: string }

/**
 * OAuth for HTTP MCP servers (design/m3-kernel.md §4.5). `mcp.authorize` answers the page to open; the Kernel
 * listens for the redirect on 127.0.0.1 and keeps the tokens in `<dataDir>/mcp-auth.json`, readable by its owner
 * only. These are a server's own tokens, not the Factory key, which never comes here.
 */
export class McpAuth {
  readonly #path: string
  readonly #authorized: (key: string) => Promise<void>
  /** Sign-ins waiting for their redirect, by the server's key (`serverKey`). */
  readonly #pending = new Map<string, Pending>()
  /** By server name and URL, as the tokens are. */
  readonly #challenges = new Map<string, Challenge>()

  constructor(dataDir: string, authorized: (key: string) => Promise<void>) {
    this.#path = join(dataDir, 'mcp-auth.json')
    this.#authorized = authorized
  }

  /** The transport's provider: refreshes a stored grant, and says when a person has to sign in again. */
  authProvider(name: string, url: string): AuthProvider {
    return adaptOAuthProvider(
      // A connection never opens a browser: it fails, and waits for mcp.authorize. The page it would have
      // opened carries the scope the server's challenge asked for (and, after an insufficient_scope, the scope
      // granted so far as well); the sign-in asks for the same.
      this.#provider(name, url, undefined, (page) => {
        const scope = page.searchParams.get('scope')
        this.#challenges.set(key(name, url), scope === null ? {} : { scope })
        throw new McpOAuthAuthorizationRequiredError()
      }),
    )
  }

  /** Start signing in: answers the page the person opens, and connects the server once the redirect comes. */
  async authorize(
    serverKey: string,
    name: string,
    url: string,
  ): Promise<{ authorizationUrl?: string; authorized?: true }> {
    this.cancel(serverKey)
    const callback = await OAuthCallbackServer.listen({
      host: '127.0.0.1',
      redirectHost: '127.0.0.1',
      timeoutMs: AUTHORIZE_TIMEOUT_MS,
    })
    const pending: Pending = { callback }
    this.#pending.set(serverKey, pending)
    const scope = this.#challenges.get(key(name, url))?.scope
    let page: URL | undefined
    try {
      const stored = this.#read()[key(name, url)]
      // A client registered for another redirect cannot finish this one: register again.
      const provider = this.#provider(name, url, callback.redirectUrl, (target) => {
        page = target
      })
      if (stored?.redirectUrl !== undefined && stored.redirectUrl !== callback.redirectUrl) {
        await provider.invalidateCredentials('client')
      }
      this.#update(name, url, (entry) => ({ ...entry, redirectUrl: callback.redirectUrl }))
      const result = await authorizeMcp(provider, {
        serverUrl: url,
        skipRefresh: true,
        ...(scope === undefined ? {} : { scope }),
      })
      if (result === 'AUTHORIZED' || page === undefined) {
        this.#finish(serverKey, pending)
        await this.#authorized(serverKey)
        return { authorized: true }
      }
      const state = page.searchParams.get('state') ?? ''
      void this.#await(serverKey, name, url, scope, pending, provider, state)
      return { authorizationUrl: page.toString() }
    } catch (failure) {
      this.#finish(serverKey, pending)
      throw failure
    }
  }

  /** `mcp.authorize.cancel`: stop listening; the page the person may still have open then fails. */
  cancel(serverKey: string): void {
    const pending = this.#pending.get(serverKey)
    if (pending !== undefined) this.#finish(serverKey, pending)
  }

  close(): void {
    for (const [serverKey, pending] of [...this.#pending]) this.#finish(serverKey, pending)
  }

  async #await(
    serverKey: string,
    name: string,
    url: string,
    scope: string | undefined,
    pending: Pending,
    provider: McpOAuthProvider,
    state: string,
  ): Promise<void> {
    try {
      const answer = await pending.callback.waitForCallback(state)
      await authorizeMcp(provider, {
        serverUrl: url,
        authorizationCode: answer.code,
        ...(answer.iss === undefined ? {} : { iss: answer.iss }),
        ...(scope === undefined ? {} : { scope }),
      })
      info('mcp server authorized', { name })
      this.#challenges.delete(key(name, url))
      this.#finish(serverKey, pending)
      await this.#authorized(serverKey)
    } catch (failure) {
      if (this.#pending.get(serverKey) === pending) {
        warn('mcp authorization did not finish', { name, error: String(failure) })
      }
      this.#finish(serverKey, pending)
    }
  }

  #finish(serverKey: string, pending: Pending): void {
    if (this.#pending.get(serverKey) === pending) this.#pending.delete(serverKey)
    void pending.callback.close().catch(() => undefined)
  }

  #provider(
    name: string,
    url: string,
    redirectUrl: string | undefined,
    onRedirect: (url: URL) => void,
  ): McpOAuthProvider {
    const store: McpOAuthStateStore = {
      load: () => this.#read()[key(name, url)]?.state,
      save: (state) => this.#update(name, url, (entry) => ({ ...entry, state })),
    }
    return new McpOAuthProvider({
      serverUrl: url,
      redirectUrl:
        redirectUrl ?? this.#read()[key(name, url)]?.redirectUrl ?? 'http://127.0.0.1/callback',
      clientMetadata: {
        client_name: 'Droi',
        grant_types: ['authorization_code', 'refresh_token'],
        response_types: ['code'],
        token_endpoint_auth_method: 'none',
      },
      store,
      onRedirect,
    })
  }

  #read(): Record<string, Stored> {
    try {
      const parsed = JSON.parse(readFileSync(this.#path, 'utf8')) as unknown
      return typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, Stored>) : {}
    } catch {
      return {}
    }
  }

  #update(name: string, url: string, change: (entry: Stored) => Stored): void {
    const all = this.#read()
    all[key(name, url)] = change(all[key(name, url)] ?? {})
    mkdirSync(dirname(this.#path), { recursive: true })
    const temporary = `${this.#path}.${process.pid}.tmp`
    writeFileSync(temporary, JSON.stringify(all, null, 2), { mode: 0o600 })
    try {
      chmodSync(temporary, 0o600)
    } catch {
      // Windows has no modes to set.
    }
    renameSync(temporary, this.#path)
  }
}

/** A server's tokens belong to its name and its exact URL: a renamed or moved server signs in again. */
function key(name: string, url: string): string {
  return `${name} ${url}`
}
