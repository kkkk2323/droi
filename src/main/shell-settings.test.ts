import { afterEach, describe, expect, test } from 'vitest'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
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

// Reversible "encryption" so tests can assert the key is not stored in clear.
const cipher = {
  encrypt: (plain: string) => Buffer.from(plain).toString('base64'),
  decrypt: (stored: string) => Buffer.from(stored, 'base64').toString(),
}

describe('shell settings', () => {
  test('creates defaults with a fresh Pairing Token on first load', () => {
    const store = createShellSettingsStore({ file: tempFile(), cipher, env: {} })
    expect(store.settings.remoteAccess).toBe(false)
    expect(store.settings.pairingToken).toMatch(/^[A-Za-z0-9_-]{32}$/)
    expect(store.settings.droidPath).toBeNull()
    expect(store.getApiKey()).toBeNull()
  })

  test('persists changes and reloads them', () => {
    const file = tempFile()
    const store = createShellSettingsStore({ file, cipher, env: {} })
    const token = store.settings.pairingToken
    store.update({ remoteAccess: true, droidPath: '/x/droid' })
    store.setApiKey('fk-test')

    const reloaded = createShellSettingsStore({ file, cipher, env: {} })
    expect(reloaded.settings).toMatchObject({
      remoteAccess: true,
      droidPath: '/x/droid',
      pairingToken: token,
    })
    expect(reloaded.getApiKey()).toBe('fk-test')
  })

  test('stores the API key encrypted, never in clear text', () => {
    const file = tempFile()
    const store = createShellSettingsStore({ file, cipher, env: {} })
    store.setApiKey('fk-secret')
    expect(readFileSync(file, 'utf8')).not.toContain('fk-secret')
  })

  test('FACTORY_API_KEY in the environment wins over the stored key', () => {
    const store = createShellSettingsStore({
      file: tempFile(),
      cipher,
      env: { FACTORY_API_KEY: 'fk-env' },
    })
    store.setApiKey('fk-stored')
    expect(store.getApiKey()).toBe('fk-env')
  })

  test('resetPairingToken produces a different token', () => {
    const store = createShellSettingsStore({ file: tempFile(), cipher, env: {} })
    const before = store.settings.pairingToken
    store.resetPairingToken()
    expect(store.settings.pairingToken).not.toBe(before)
  })

  test('tolerates a corrupt file by starting over', () => {
    const file = tempFile()
    const store = createShellSettingsStore({ file, cipher, env: {} })
    store.update({ remoteAccess: true })
    writeFileSync(file, '{not json')
    const reloaded = createShellSettingsStore({ file, cipher, env: {} })
    expect(reloaded.settings.remoteAccess).toBe(false)
  })
})
