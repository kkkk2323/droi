import test from 'node:test'
import assert from 'node:assert/strict'
import { JsonRpcLineParser } from '../src/backend/droid/jsonrpc/jsonRpcLineParser.ts'

test('JsonRpcLineParser parses JSON-RPC lines and stdout fallbacks', () => {
  const p = new JsonRpcLineParser()
  const out1 = p.push('{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"notification","method":"droid.session_notification","params":{"notification":{"type":"assistant_text_delta","messageId":"m1","blockIndex":0,"textDelta":"hi"}}}\nnot-json\n')
  assert.equal(out1.length, 2)
  assert.equal(out1[0].kind, 'message')
  assert.equal((out1[0] as any).message.type, 'notification')
  assert.equal((out1[1] as any).kind, 'stdout')
  assert.equal((out1[1] as any).data, 'not-json')

  const out2 = p.push('{"jsonrpc":"2.0","factoryApiVersion":"1.0.0","type":"request","id":"1","method":"droid.initialize_session","params":{"machineId":"x","cwd":"/repo"}}')
  assert.deepEqual(out2, [])

  const out3 = p.flush()
  assert.equal(out3.length, 1)
  assert.equal(out3[0].kind, 'message')
  assert.equal((out3[0] as any).message.type, 'request')
})

