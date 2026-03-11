import test from 'node:test'
import assert from 'node:assert/strict'

import { resolveSessionProtocolFields } from '../src/shared/sessionProtocol.ts'

test('normal sessions let the current autoLevel override stale explicit protocol fields', () => {
  const protocol = resolveSessionProtocolFields({
    autoLevel: 'default',
    explicit: {
      sessionKind: 'normal',
      interactionMode: 'auto',
      autonomyLevel: 'medium',
    },
  })

  assert.deepEqual(protocol, {
    isMission: false,
    sessionKind: 'normal',
    interactionMode: 'spec',
    autonomyLevel: 'off',
  })
})

test('mission sessions still preserve orchestrator protocol when autoLevel changes', () => {
  const protocol = resolveSessionProtocolFields({
    autoLevel: 'default',
    explicit: {
      isMission: true,
      sessionKind: 'mission',
      interactionMode: 'agi',
      autonomyLevel: 'high',
      decompSessionType: 'orchestrator',
    },
  })

  assert.deepEqual(protocol, {
    isMission: true,
    sessionKind: 'mission',
    interactionMode: 'agi',
    autonomyLevel: 'high',
    decompSessionType: 'orchestrator',
  })
})
