import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createAppStateStore } from '../src/backend/storage/appStateStore.ts'
import { createKeyStore } from '../src/backend/keys/keyStore.ts'

function installUsageFetchMock(
  entries: Record<string, { used: number; total: number; expiresAtMs?: number; status?: number }>,
): () => void {
  const prevFetch = globalThis.fetch
  globalThis.fetch = (async (_input: string | URL | Request, init?: RequestInit) => {
    const auth = new Headers(init?.headers).get('Authorization') || ''
    const match = /^Bearer\s+(.+)$/i.exec(auth)
    const key = match?.[1] || ''
    const entry = entries[key]
    if (!entry) return new Response('', { status: 401 })
    if (entry.status && entry.status !== 200) return new Response('', { status: entry.status })
    return new Response(
      JSON.stringify({
        usage: {
          total: {
            totalAllowance: entry.total,
            orgTotalTokensUsed: entry.used,
            orgOverageUsed: 0,
          },
          endDate: entry.expiresAtMs || Date.UTC(2026, 2, 20),
        },
      }),
      {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      },
    )
  }) as typeof fetch
  return () => {
    globalThis.fetch = prevFetch
  }
}

async function createStoreWithKeys(keys: string[]) {
  const dir = await mkdtemp(join(tmpdir(), 'droid-key-store-'))
  const appStateStore = createAppStateStore({ baseDir: dir })
  await appStateStore.save({
    version: 2,
    machineId: 'm-test',
    apiKeys: keys.map((key, index) => ({ key, addedAt: index + 1 })),
  })
  return { appStateStore, keyStore: createKeyStore(appStateStore) }
}

test('keyStore keeps a session on the same key until it reaches 98%', async () => {
  const restoreFetch = installUsageFetchMock({
    'fk-a': { used: 40, total: 100, expiresAtMs: Date.UTC(2026, 2, 20) },
    'fk-b': { used: 10, total: 100, expiresAtMs: Date.UTC(2026, 2, 21) },
  })
  const { keyStore } = await createStoreWithKeys(['fk-a', 'fk-b'])

  assert.equal(await keyStore.getActiveKey('session-1'), 'fk-a')

  restoreFetch()
  const restoreFetch2 = installUsageFetchMock({
    'fk-a': { used: 97, total: 100, expiresAtMs: Date.UTC(2026, 2, 20) },
    'fk-b': { used: 10, total: 100, expiresAtMs: Date.UTC(2026, 2, 21) },
  })
  keyStore.invalidateUsages()
  assert.equal(await keyStore.getActiveKey('session-1'), 'fk-a')

  restoreFetch2()
  const restoreFetch3 = installUsageFetchMock({
    'fk-a': { used: 98, total: 100, expiresAtMs: Date.UTC(2026, 2, 20) },
    'fk-b': { used: 10, total: 100, expiresAtMs: Date.UTC(2026, 2, 21) },
  })
  keyStore.invalidateUsages()
  assert.equal(await keyStore.getActiveKey('session-1'), 'fk-b')
  assert.equal(await keyStore.getBoundKey('session-1'), 'fk-b')

  restoreFetch3()
})

test('keyStore prunes bindings for removed keys and reselects on next use', async () => {
  const restoreFetch = installUsageFetchMock({
    'fk-a': { used: 20, total: 100, expiresAtMs: Date.UTC(2026, 2, 20) },
    'fk-b': { used: 30, total: 100, expiresAtMs: Date.UTC(2026, 2, 21) },
  })
  const { keyStore } = await createStoreWithKeys(['fk-a', 'fk-b'])

  assert.equal(await keyStore.getActiveKey('session-1'), 'fk-a')
  await keyStore.removeKey(0)
  assert.equal(await keyStore.getBoundKey('session-1'), null)
  keyStore.invalidateUsages()
  assert.equal(await keyStore.getActiveKey('session-1'), 'fk-b')

  restoreFetch()
})

test('keyStore can move a binding to a replacement session id', async () => {
  const { keyStore } = await createStoreWithKeys(['fk-a'])
  await keyStore.bindSessionKey('session-old', 'fk-a')

  await keyStore.moveSessionBinding('session-old', 'session-new')

  assert.equal(await keyStore.getBoundKey('session-old'), null)
  assert.equal(await keyStore.getBoundKey('session-new'), 'fk-a')
})
