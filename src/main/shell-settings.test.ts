import { afterEach, describe, expect, test } from 'vitest'
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createShellSettingsStore } from './shell-settings'

const dirs: string[] = []
function tempFile(): string {
  const dir = mkdtempSync(join(tmpdir(), 'droi-settings-'))
  dirs.push(dir)
  return join(dir, 'settings.json')
}

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true })
})

describe('shell settings', () => {
  test('creates defaults with a fresh Pairing Token on first load', () => {
    const store = createShellSettingsStore({ file: tempFile(), env: {} })
    expect(store.settings.remoteAccess).toBe(false)
    expect(store.settings.pairingToken).toMatch(/^[A-Za-z0-9_-]{32}$/)
    expect(store.settings.droidPath).toBeNull()
    expect(store.settings.factoryApiBaseUrl).toBeNull()
    expect(store.getApiKey()).toBeNull()
    expect(store.getLogin()).toBeNull()
  })

  test('persists changes and reloads them', () => {
    const file = tempFile()
    const store = createShellSettingsStore({ file, env: {} })
    const token = store.settings.pairingToken
    store.update({
      remoteAccess: true,
      droidPath: '/x/droid',
      factoryApiBaseUrl: 'http://127.0.0.1:37650',
    })
    store.setApiKey('fk-test')
    store.setLogin('{"access":"a"}')

    const reloaded = createShellSettingsStore({ file, env: {} })
    expect(reloaded.settings).toMatchObject({
      remoteAccess: true,
      droidPath: '/x/droid',
      factoryApiBaseUrl: 'http://127.0.0.1:37650',
      pairingToken: token,
    })
    expect(reloaded.getApiKey()).toBe('fk-test')
    expect(reloaded.getLogin()).toBe('{"access":"a"}')
  })

  test('the file is only readable by its owner', () => {
    const file = tempFile()
    createShellSettingsStore({ file, env: {} })
    expect(statSync(file).mode & 0o777).toBe(0o600)
  })

  test('moves secrets out of the old safeStorage layout on first load', () => {
    const file = tempFile()
    const enc = (plain: string) => Buffer.from(plain).toString('base64')
    writeFileSync(
      file,
      JSON.stringify({
        remoteAccess: true,
        pairingTokenEncrypted: enc('old-pairing-token'),
        apiKeyEncrypted: enc('fk-old'),
        loginEncrypted: enc('{"refresh":"r"}'),
      }),
    )
    const store = createShellSettingsStore({
      file,
      env: {},
      decryptLegacy: (stored) => Buffer.from(stored, 'base64').toString(),
    })
    expect(store.settings.pairingToken).toBe('old-pairing-token')
    expect(store.getApiKey()).toBe('fk-old')
    expect(store.getLogin()).toBe('{"refresh":"r"}')
    const written = JSON.parse(readFileSync(file, 'utf8'))
    expect(written).toMatchObject({ pairingToken: 'old-pairing-token', apiKey: 'fk-old' })
    expect(written).not.toHaveProperty('pairingTokenEncrypted')
  })

  test('an old layout that cannot be decrypted starts over instead of failing', () => {
    const file = tempFile()
    writeFileSync(file, JSON.stringify({ pairingTokenEncrypted: 'garbage', loginEncrypted: 'x' }))
    const store = createShellSettingsStore({
      file,
      env: {},
      decryptLegacy: () => {
        throw new Error('keychain says no')
      },
    })
    expect(store.settings.pairingToken).toMatch(/^[A-Za-z0-9_-]{32}$/)
    expect(store.getLogin()).toBeNull()
  })

  test('FACTORY_API_KEY in the environment wins over the stored key', () => {
    const store = createShellSettingsStore({ file: tempFile(), env: { FACTORY_API_KEY: 'fk-env' } })
    store.setApiKey('fk-stored')
    expect(store.getApiKey()).toBe('fk-env')
  })

  test('resetPairingToken produces a different token', () => {
    const store = createShellSettingsStore({ file: tempFile(), env: {} })
    const before = store.settings.pairingToken
    store.resetPairingToken()
    expect(store.settings.pairingToken).not.toBe(before)
  })

  test('tolerates a corrupt file by starting over', () => {
    const file = tempFile()
    const store = createShellSettingsStore({ file, env: {} })
    store.update({ remoteAccess: true })
    writeFileSync(file, '{not json')
    const reloaded = createShellSettingsStore({ file, env: {} })
    expect(reloaded.settings.remoteAccess).toBe(false)
  })
})
