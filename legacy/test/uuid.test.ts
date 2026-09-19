import test from 'node:test'
import assert from 'node:assert/strict'

import { uuidFromRandomBytes, uuidv4 } from '../src/renderer/src/lib/uuid.ts'

function withCryptoOverride<T>(cryptoValue: any, fn: () => T): T {
  const orig = Object.getOwnPropertyDescriptor(globalThis, 'crypto')
  Object.defineProperty(globalThis, 'crypto', {
    value: cryptoValue,
    configurable: true,
    enumerable: true,
    writable: true,
  })
  try {
    return fn()
  } finally {
    if (orig) Object.defineProperty(globalThis, 'crypto', orig)
    else delete (globalThis as any).crypto
  }
}

test('uuidv4 prefers crypto.randomUUID when available', () => {
  const id = withCryptoOverride({ randomUUID: () => 'test-uuid' }, () => uuidv4())
  assert.equal(id, 'test-uuid')
})

test('uuidv4 falls back to getRandomValues and emits RFC4122 v4', () => {
  const seeded = new Uint8Array(16)
  for (let i = 0; i < seeded.length; i++) seeded[i] = i

  const expected = uuidFromRandomBytes(seeded)
  const id = withCryptoOverride({
    getRandomValues: (arr: Uint8Array) => {
      arr.set(seeded)
      return arr
    },
  }, () => uuidv4())

  assert.equal(id, expected)
  assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
})
