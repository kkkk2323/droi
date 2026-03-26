import test from 'node:test'
import assert from 'node:assert/strict'
import { getMissingDroidHooks } from '../src/renderer/src/lib/droidHooks.ts'

test('getMissingDroidHooks returns empty when all required hooks exist', () => {
  const client = {
    onRpcNotification: () => {},
    onRpcRequest: () => {},
    onMissionDirChanged: () => {},
    onTurnEnd: () => {},
    onStdout: () => {},
    onStderr: () => {},
    onError: () => {},
    onSetupScriptEvent: () => {},
  }

  assert.deepEqual(getMissingDroidHooks(client), [])
})

test('getMissingDroidHooks returns missing required hooks', () => {
  const client = {
    onRpcNotification: () => {},
    onTurnEnd: () => {},
    onError: 'nope',
  }

  assert.deepEqual(getMissingDroidHooks(client), [
    'onRpcRequest',
    'onMissionDirChanged',
    'onStdout',
    'onStderr',
    'onError',
    'onSetupScriptEvent',
  ])
})
