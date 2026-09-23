// The `droid` CLI's own login, read from its credential store so the Desktop
// Shell needs no sign-in of its own: the Daemon already runs as this login, so
// its access token is exactly what `daemon.authenticate` wants as `token`.
//
// Layout of the store (all under ~/.factory, written by the CLI):
//   auth.v2.loginkeychain  credentials; key in the macOS login keychain
//                          (service "Factory CLI", account
//                          "auth-encryption-key-security-cli"), read with
//                          /usr/bin/security like the CLI itself does
//   auth.v2.keyring        credentials; key stored through keytar under the
//                          account "auth-encryption-key"
//   auth.v2.file           credentials; key next to it in auth.v2.key
// Each credentials file is AES-256-GCM as `iv:tag:ciphertext` (base64) over
// JSON `{ access_token, refresh_token, active_organization_id }`.
//
// The token is never refreshed here: the CLI rotates refresh tokens, so doing
// it from a second process would log the CLI out. The running Daemon keeps
// the file fresh; this module re-reads it whenever it changes.
import { execFile } from 'node:child_process'
import { createDecipheriv } from 'node:crypto'
import { readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import type { FactoryAccount } from './factory-auth'
import { decodeJwt } from './factory-auth'

export interface CliLogin {
  accessToken: string
  account: FactoryAccount
}

export interface CliLoginReader {
  /** The CLI's current login, or null when it is logged out or unreadable. */
  read(): Promise<CliLogin | null>
}

export interface CliLoginOptions {
  /** The CLI's home, normally ~/.factory. */
  factoryHome: string
  /** Reads a key from the secure store; null when there is none. */
  readSecureKey?: (account: string) => Promise<string | null>
}

const KEYCHAIN_SERVICE = 'Factory CLI'
const SECURITY_CLI_ACCOUNT = 'auth-encryption-key-security-cli'
const KEYRING_ACCOUNT = 'auth-encryption-key'
const KEY_BYTES = 32
const IV_BYTES = 16
const TAG_BYTES = 16

export function createCliLoginReader(options: CliLoginOptions): CliLoginReader {
  const readSecureKey = options.readSecureKey ?? defaultSecureKeyReader()
  const secureKeys = new Map<string, Buffer | null>()
  let cached: { identity: string; login: CliLogin | null } | null = null

  const secureKey = async (account: string): Promise<Buffer | null> => {
    const known = secureKeys.get(account)
    if (known !== undefined) return known
    let key: Buffer | null = null
    try {
      const encoded = await readSecureKey(account)
      if (encoded) key = Buffer.from(encoded.trim(), 'base64')
    } catch {
      key = null
    }
    if (key && key.length !== KEY_BYTES) key = null
    // A missing key is remembered too: the CLI never creates one without also
    // writing the credentials file, so asking again would only spawn processes.
    secureKeys.set(account, key)
    return key
  }

  const sources: Array<{ file: string; key: () => Promise<Buffer | null> }> = [
    { file: 'auth.v2.loginkeychain', key: () => secureKey(SECURITY_CLI_ACCOUNT) },
    { file: 'auth.v2.keyring', key: () => secureKey(KEYRING_ACCOUNT) },
    {
      file: 'auth.v2.file',
      key: async () => readKeyFile(join(options.factoryHome, 'auth.v2.key')),
    },
  ]

  return {
    async read() {
      const identity = sources.map((s) => fileIdentity(join(options.factoryHome, s.file))).join('|')
      if (cached && cached.identity === identity) return cached.login
      let login: CliLogin | null = null
      for (const source of sources) {
        const path = join(options.factoryHome, source.file)
        const encrypted = readText(path)
        if (!encrypted) continue
        const key = await source.key()
        if (!key) continue
        login = parseCredentials(decrypt(encrypted, key), options.factoryHome)
        if (login) break
      }
      cached = { identity, login }
      return login
    },
  }
}

/** macOS: the same `security` call the CLI makes, so no keychain prompt appears. */
function defaultSecureKeyReader(): (account: string) => Promise<string | null> {
  if (process.platform !== 'darwin') return async () => null
  return (account) =>
    new Promise((resolve) => {
      execFile(
        '/usr/bin/security',
        ['find-generic-password', '-s', KEYCHAIN_SERVICE, '-a', account, '-w'],
        { timeout: 10_000 },
        (error, stdout) => resolve(error ? null : stdout.trim()),
      )
    })
}

export function decrypt(encrypted: string, key: Buffer): string | null {
  const parts = encrypted.trim().split(':')
  if (parts.length !== 3) return null
  const [iv, tag, ciphertext] = parts.map((p) => Buffer.from(p, 'base64')) as [
    Buffer,
    Buffer,
    Buffer,
  ]
  if (iv.length !== IV_BYTES || tag.length !== TAG_BYTES) return null
  try {
    const decipher = createDecipheriv('aes-256-gcm', key, iv)
    decipher.setAuthTag(tag)
    return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString('utf8')
  } catch {
    return null
  }
}

function parseCredentials(json: string | null, factoryHome: string): CliLogin | null {
  if (!json) return null
  let parsed: { access_token?: unknown; active_organization_id?: unknown }
  try {
    parsed = JSON.parse(json) as typeof parsed
  } catch {
    return null
  }
  if (typeof parsed.access_token !== 'string' || !parsed.access_token) return null
  const claims = decodeJwt(parsed.access_token)
  // The Daemon compares Factory's own ids, which the CLI records in host.json
  // when it registers this computer; the JWT only carries the WorkOS ids.
  const registration = readRegistration(factoryHome)
  return {
    accessToken: parsed.access_token,
    account: {
      userId:
        registration?.userId ?? (typeof claims['sub'] === 'string' ? claims['sub'] : 'unknown'),
      orgId:
        typeof parsed.active_organization_id === 'string'
          ? parsed.active_organization_id
          : (registration?.orgId ?? null),
      email: typeof claims['email'] === 'string' ? claims['email'] : null,
    },
  }
}

/** Who the CLI registered this computer as (~/.factory/host.json). */
export function readRegistration(
  factoryHome: string,
): { userId: string; orgId: string | null } | null {
  try {
    const host = JSON.parse(readFileSync(join(factoryHome, 'host.json'), 'utf8')) as {
      computerRegistration?: { userId?: unknown; firestoreOrgId?: unknown }
    }
    const registration = host.computerRegistration
    if (typeof registration?.userId !== 'string') return null
    return {
      userId: registration.userId,
      orgId: typeof registration.firestoreOrgId === 'string' ? registration.firestoreOrgId : null,
    }
  } catch {
    return null
  }
}

function readKeyFile(path: string): Buffer | null {
  const text = readText(path)
  if (!text) return null
  const key = Buffer.from(text.trim(), 'base64')
  return key.length === KEY_BYTES ? key : null
}

function readText(path: string): string | null {
  try {
    return readFileSync(path, 'utf8')
  } catch {
    return null
  }
}

function fileIdentity(path: string): string {
  try {
    const stat = statSync(path, { bigint: true })
    return `${stat.ino}:${stat.mtimeNs}:${stat.size}`
  } catch {
    return '-'
  }
}
