import test from 'node:test'
import assert from 'node:assert/strict'
import { selectActiveKey } from '../src/backend/keys/keyUsageService.ts'
import type { ApiKeyEntry, ApiKeyUsage } from '../src/shared/protocol.ts'

function makeKey(key: string, addedAt = 1): ApiKeyEntry {
  return { key, addedAt }
}

function makeUsage(params: {
  used: number
  total: number
  expires: string
  expiresTs: number
}): ApiKeyUsage {
  return {
    used: params.used,
    total: params.total,
    expires: params.expires,
    expiresTs: params.expiresTs,
    lastCheckedAt: Date.now(),
  }
}

test('selectActiveKey prefers the earliest expiry before lower usage', () => {
  const keys = [makeKey('fk-early'), makeKey('fk-late')]
  const usages = new Map<string, ApiKeyUsage>([
    [
      'fk-early',
      makeUsage({ used: 60, total: 100, expires: '2026-03-20', expiresTs: 1_742_428_800 }),
    ],
    [
      'fk-late',
      makeUsage({ used: 10, total: 100, expires: '2026-04-20', expiresTs: 1_745_020_800 }),
    ],
  ])

  assert.deepEqual(selectActiveKey(keys, usages), { key: 'fk-early', index: 0 })
})

test('selectActiveKey breaks same-expiry ties with the lowest usage ratio', () => {
  const keys = [makeKey('fk-busier'), makeKey('fk-lighter')]
  const usages = new Map<string, ApiKeyUsage>([
    [
      'fk-busier',
      makeUsage({ used: 55, total: 100, expires: '2026-03-20', expiresTs: 1_742_428_800 }),
    ],
    [
      'fk-lighter',
      makeUsage({ used: 25, total: 100, expires: '2026-03-20', expiresTs: 1_742_428_800 }),
    ],
  ])

  assert.deepEqual(selectActiveKey(keys, usages), { key: 'fk-lighter', index: 1 })
})

test('selectActiveKey rotates away once a key reaches the 98% threshold', () => {
  const keys = [makeKey('fk-nearly-full'), makeKey('fk-next')]
  const usages = new Map<string, ApiKeyUsage>([
    [
      'fk-nearly-full',
      makeUsage({ used: 98, total: 100, expires: '2026-03-20', expiresTs: 1_742_428_800 }),
    ],
    [
      'fk-next',
      makeUsage({ used: 40, total: 100, expires: '2026-03-21', expiresTs: 1_742_515_200 }),
    ],
  ])

  assert.deepEqual(selectActiveKey(keys, usages), { key: 'fk-next', index: 1 })
})
