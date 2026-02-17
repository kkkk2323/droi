import test from 'node:test'
import assert from 'node:assert/strict'

import { resolvePairingPort } from '../src/server/apiServer.ts'

test('resolvePairingPort prefers pairingWebPort when valid', () => {
  assert.equal(resolvePairingPort(3001, 5173), 5173)
  assert.equal(resolvePairingPort(3001, undefined), 3001)
  assert.equal(resolvePairingPort(3001, 0), 3001)
  assert.equal(resolvePairingPort(3001, -1), 3001)
})
