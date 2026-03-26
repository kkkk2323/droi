import test from 'node:test'
import assert from 'node:assert/strict'
import { getMissingDroidHooks } from '../src/renderer/src/lib/droidHooks.ts'

test('getMissingDroidHooks returns empty when all required hooks exist', () => {
  const client = {
    onMessage: () => {},
    onPermissionRequest: () => {},
    onAskUserRequest: () => {},
    onMissionDirChanged: () => {},
    onTurnEnd: () => {},
    onError: () => {},
    onSetupScriptEvent: () => {},
  }

  assert.deepEqual(getMissingDroidHooks(client), [])
})

test('getMissingDroidHooks returns missing required hooks', () => {
  const client = {
    onMessage: () => {},
    onTurnEnd: () => {},
    onError: 'nope',
  }

  assert.deepEqual(getMissingDroidHooks(client), [
    'onPermissionRequest',
    'onAskUserRequest',
    'onMissionDirChanged',
    'onError',
    'onSetupScriptEvent',
  ])
})
