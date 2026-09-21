import { afterEach, describe, expect, test } from 'vitest'
import { createCipheriv, randomBytes } from 'node:crypto'
import { mkdtempSync, rmSync, utimesSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createCliLoginReader, decrypt } from './cli-login'

const dirs: string[] = []
function tempHome(): string {
  const dir = mkdtempSync(join(tmpdir(), 'droi-factory-home-'))
  dirs.push(dir)
  return dir
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true })
})

function encrypt(text: string, key: Buffer): string {
  const iv = randomBytes(16)
  const cipher = createCipheriv('aes-256-gcm', key, iv)
  const ciphertext = Buffer.concat([cipher.update(text, 'utf8'), cipher.final()])
  return `${iv.toString('base64')}:${cipher.getAuthTag().toString('base64')}:${ciphertext.toString('base64')}`
}

function jwt(claims: Record<string, unknown>): string {
  const b64 = (o: unknown) => Buffer.from(JSON.stringify(o)).toString('base64url')
  return `${b64({ alg: 'RS256' })}.${b64(claims)}.sig`
}

const credentials = (accessToken: string) =>
  JSON.stringify({
    access_token: accessToken,
    refresh_token: 'r',
    active_organization_id: 'org-firestore',
  })

describe('decrypt', () => {
  test('round-trips the CLI format', () => {
    const key = randomBytes(32)
    expect(decrypt(encrypt('hello', key), key)).toBe('hello')
  })

  test('returns null for a wrong key or a malformed value', () => {
    const key = randomBytes(32)
    expect(decrypt(encrypt('hello', key), randomBytes(32))).toBeNull()
    expect(decrypt('not:encrypted', key)).toBeNull()
    expect(decrypt('a:b:c', key)).toBeNull()
  })
})

describe('createCliLoginReader', () => {
  test('reads auth.v2.loginkeychain with the key from the secure store', async () => {
    const home = tempHome()
    const key = randomBytes(32)
    const token = jwt({ sub: 'user_workos', email: 'dev@example.com' })
    writeFileSync(join(home, 'auth.v2.loginkeychain'), encrypt(credentials(token), key))
    writeFileSync(
      join(home, 'host.json'),
      JSON.stringify({ computerRegistration: { userId: 'user-firestore', firestoreOrgId: 'o' } }),
    )
    const asked: string[] = []
    const reader = createCliLoginReader({
      factoryHome: home,
      readSecureKey: async (account) => {
        asked.push(account)
        return account === 'auth-encryption-key-security-cli' ? key.toString('base64') : null
      },
    })
    expect(await reader.read()).toEqual({
      accessToken: token,
      account: { userId: 'user-firestore', orgId: 'org-firestore', email: 'dev@example.com' },
    })
    expect(asked).toEqual(['auth-encryption-key-security-cli'])
  })

  test('falls back to auth.v2.file with auth.v2.key and the JWT subject', async () => {
    const home = tempHome()
    const key = randomBytes(32)
    const token = jwt({ sub: 'user_workos' })
    writeFileSync(join(home, 'auth.v2.file'), encrypt(credentials(token), key))
    writeFileSync(join(home, 'auth.v2.key'), key.toString('base64'))
    const reader = createCliLoginReader({ factoryHome: home, readSecureKey: async () => null })
    expect(await reader.read()).toMatchObject({
      accessToken: token,
      account: { userId: 'user_workos', orgId: 'org-firestore', email: null },
    })
  })

  test('is null when the CLI is logged out or the key is missing', async () => {
    const home = tempHome()
    const reader = createCliLoginReader({ factoryHome: home, readSecureKey: async () => null })
    expect(await reader.read()).toBeNull()
    writeFileSync(join(home, 'auth.v2.loginkeychain'), encrypt('{}', randomBytes(32)))
    expect(await reader.read()).toBeNull()
  })

  test('re-reads only when a credentials file changes, and asks the secure store once', async () => {
    const home = tempHome()
    const key = randomBytes(32)
    const file = join(home, 'auth.v2.loginkeychain')
    writeFileSync(file, encrypt(credentials(jwt({ sub: 'a' })), key))
    let asked = 0
    const reader = createCliLoginReader({
      factoryHome: home,
      readSecureKey: async () => {
        asked += 1
        return key.toString('base64')
      },
    })
    const first = await reader.read()
    expect(await reader.read()).toBe(first)
    writeFileSync(file, encrypt(credentials(jwt({ sub: 'b' })), key))
    // Same second on coarse filesystems: force a distinct mtime.
    utimesSync(file, new Date(Date.now() + 5_000), new Date(Date.now() + 5_000))
    const second = await reader.read()
    expect(second?.account.userId).toBe('b')
    expect(asked).toBe(1)
  })
})
