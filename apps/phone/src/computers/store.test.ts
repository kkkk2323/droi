import { preferenceStorage } from '@droi/daemon-layer/local-preference'
import { beforeEach, describe, expect, test } from 'vitest'
import {
  mergeComputer,
  pairedComputers,
  removeComputer,
  sessionSummaries,
  savePairing,
  selectedComputer,
  selectedComputerId,
  tokenKey,
  type Keychain,
} from './store'

function fakeKeychain(): Keychain & { secrets: Map<string, string> } {
  const secrets = new Map<string, string>()
  return {
    secrets,
    get: async (key) => secrets.get(key) ?? null,
    set: async (key, value) => void secrets.set(key, value),
    delete: async (key) => void secrets.delete(key),
  }
}

const home = { id: 'c-home', name: 'Home Mac', address: 'http://192.168.1.10:41417' }

beforeEach(() => {
  pairedComputers.set([])
  selectedComputerId.set(null)
})

describe('Paired Computer store', () => {
  test('pairing keeps the token in the keychain only and selects the computer', async () => {
    const keychain = fakeKeychain()
    await savePairing({ ...home, token: 'secret-token' }, keychain)
    expect(keychain.secrets.get(tokenKey('c-home'))).toBe('secret-token')
    expect(pairedComputers.get()).toEqual([home])
    expect(selectedComputerId.get()).toBe('c-home')
    expect(preferenceStorage().getItem('droi.pairedComputers')).not.toContain('secret-token')
  })

  test('a known id updates the address and token and keeps the given name', async () => {
    const keychain = fakeKeychain()
    await savePairing({ ...home, token: 'old' }, keychain)
    pairedComputers.set([{ ...home, name: 'My desk' }])
    await savePairing(
      { id: 'c-home', name: 'Home Mac', address: 'http://mac.tail1.ts.net:41417', token: 'new' },
      keychain,
    )
    expect(pairedComputers.get()).toEqual([
      { id: 'c-home', name: 'My desk', address: 'http://mac.tail1.ts.net:41417' },
    ])
    expect(keychain.secrets.get(tokenKey('c-home'))).toBe('new')
  })

  test('another id is another computer', () => {
    const office = { id: 'c-office', name: 'Office', address: 'http://10.0.0.5:41417' }
    expect(mergeComputer([home], office)).toEqual([home, office])
  })

  test('the selection falls back to the first computer', () => {
    expect(selectedComputer([home], 'gone')).toEqual(home)
    expect(selectedComputer([], null)).toBeNull()
  })
})

describe('removing a computer', () => {
  test('forgets its token and summary and selects another', async () => {
    const keychain = fakeKeychain()
    const office = { id: 'c-office', name: 'Office', address: 'http://10.0.0.5:41417' }
    await savePairing({ ...home, token: 'a' }, keychain)
    await savePairing({ ...office, token: 'b' }, keychain)
    sessionSummaries('c-office').set([
      {
        sessionId: 's',
        title: 'T',
        cwd: '/x',
        repoRoot: null,
        updatedAt: 1,
        archivedAt: null,
        parentId: null,
      },
    ])
    await removeComputer('c-office', keychain)
    expect(pairedComputers.get()).toEqual([home])
    expect(selectedComputerId.get()).toBe('c-home')
    expect(keychain.secrets.has(tokenKey('c-office'))).toBe(false)
    expect(sessionSummaries('c-office').get()).toEqual([])
  })
})
