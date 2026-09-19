import test from 'node:test'
import assert from 'node:assert/strict'
import { shouldRequireAuth } from '../src/server/apiServer.ts'

test('shouldRequireAuth always returns false (auth disabled)', () => {
  const remote = '192.168.1.10'
  assert.equal(shouldRequireAuth({ remoteAddress: remote, method: 'GET', path: '/' }), false)
  assert.equal(shouldRequireAuth({ remoteAddress: remote, method: 'GET', path: '/api/version' }), false)
  assert.equal(shouldRequireAuth({ remoteAddress: remote, method: 'POST', path: '/api/exec' }), false)
  assert.equal(shouldRequireAuth({ remoteAddress: remote, method: 'GET', path: '/mobile/connections' }), false)
})

test('shouldRequireAuth does not require auth for loopback', () => {
  assert.equal(shouldRequireAuth({ remoteAddress: '127.0.0.1', method: 'POST', path: '/api/exec' }), false)
  assert.equal(shouldRequireAuth({ remoteAddress: '::1', method: 'GET', path: '/api/version' }), false)
})

