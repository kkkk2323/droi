import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildNotificationTraceInfo,
  computeNotificationFingerprint,
  formatNotificationTrace,
} from '../src/backend/droid/jsonrpc/notificationFingerprint.ts'

const baseNotification = {
  jsonrpc: '2.0',
  factoryApiVersion: '1.0.0',
  type: 'notification',
  method: 'droid.session_notification',
  params: {
    notification: {
      type: 'assistant_text_delta',
      messageId: 'm1',
      blockIndex: 0,
      textDelta: 'hi',
    },
  },
} as const

test('notification fingerprint remains stable when object key order changes', () => {
  const a = {
    ...baseNotification,
    params: {
      notification: {
        messageId: 'm1',
        type: 'assistant_text_delta',
        textDelta: 'hi',
        blockIndex: 0,
      },
    },
  } as any

  const b = {
    factoryApiVersion: '1.0.0',
    type: 'notification',
    method: 'droid.session_notification',
    jsonrpc: '2.0',
    params: {
      notification: {
        blockIndex: 0,
        textDelta: 'hi',
        type: 'assistant_text_delta',
        messageId: 'm1',
      },
    },
  } as any

  assert.equal(computeNotificationFingerprint(a), computeNotificationFingerprint(b))
})

test('notification fingerprint changes when key fields change', () => {
  const a = baseNotification as any
  const b = {
    ...baseNotification,
    params: {
      notification: {
        ...baseNotification.params.notification,
        textDelta: 'hello',
      },
    },
  } as any
  assert.notEqual(computeNotificationFingerprint(a), computeNotificationFingerprint(b))
})

test('trace formatter includes required chain fields', () => {
  const notification = {
    ...baseNotification,
    _meta: { traceparent: '00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01' },
    params: {
      notification: {
        ...baseNotification.params.notification,
      },
    },
  } as any

  const info = buildNotificationTraceInfo(notification)
  const line = formatNotificationTrace('session-in', notification)

  assert.equal(info.type, 'assistant_text_delta')
  assert.equal(info.messageId, 'm1')
  assert.equal(info.blockIndex, '0')
  assert.equal(info.deltaLen, '2')
  assert.match(line, /trace-chain: stage=session-in/)
  assert.match(line, new RegExp(`fingerprint=${info.fingerprint}`))
  assert.match(line, /method=droid\.session_notification/)
})
