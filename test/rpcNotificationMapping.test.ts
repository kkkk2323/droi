import test from 'node:test'
import assert from 'node:assert/strict'
import { makeBuffer, applyRpcNotification, applyRpcRequest } from '../src/renderer/src/state/appReducer.ts'
import { buildNotificationTraceInfo, formatNotificationTrace } from '../src/renderer/src/lib/notificationFingerprint.ts'

const baseNotif = {
  jsonrpc: '2.0',
  factoryApiVersion: '1.0.0',
  type: 'notification',
  method: 'droid.session_notification',
} as const

test('trace fingerprint is stable for duplicate notification payloads', () => {
  const notif = {
    ...baseNotif,
    params: {
      notification: {
        type: 'assistant_text_delta',
        messageId: 'm-trace',
        blockIndex: 0,
        textDelta: 'hello',
      },
    },
  } as any

  const clone = JSON.parse(JSON.stringify(notif))
  const info1 = buildNotificationTraceInfo(notif)
  const info2 = buildNotificationTraceInfo(clone)
  assert.equal(info1.fingerprint, info2.fingerprint)

  const line = formatNotificationTrace('renderer-in', notif)
  assert.match(line, /trace-chain: stage=renderer-in/)
  assert.match(line, new RegExp(`fingerprint=${info1.fingerprint}`))
})

test('applyRpcNotification appends assistant_text_delta by messageId/blockIndex', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])
  const next1 = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'assistant_text_delta', messageId: 'm1', blockIndex: 0, textDelta: 'hi' } },
  } as any)
  const buf1 = next1.get(sid)!
  assert.equal(buf1.messages.length, 1)
  assert.equal(buf1.messages[0].id, 'droid:m1')
  assert.equal((buf1.messages[0].blocks[0] as any).content, 'hi')

  const next2 = applyRpcNotification(next1, sid, {
    ...baseNotif,
    params: { notification: { type: 'assistant_text_delta', messageId: 'm1', blockIndex: 0, textDelta: ' there' } },
  } as any)
  const buf2 = next2.get(sid)!
  assert.equal((buf2.messages[0].blocks[0] as any).content, 'hi there')
})

test('applyRpcNotification maps tool_use/tool_result to ToolCallBlock', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])
  const withTool = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'tool_use', id: 't1', name: 'Execute', input: { command: 'ls' } } },
  } as any)
  const buf1 = withTool.get(sid)!
  const toolBlock = buf1.messages[0].blocks[0] as any
  assert.equal(toolBlock.kind, 'tool_call')
  assert.equal(toolBlock.callId, 't1')
  assert.equal(toolBlock.toolName, 'Execute')

  const withResult = applyRpcNotification(withTool, sid, {
    ...baseNotif,
    params: { notification: { type: 'tool_result', toolUseId: 't1', content: 'ok', isError: false } },
  } as any)
  const buf2 = withResult.get(sid)!
  const toolBlock2 = buf2.messages[0].blocks[0] as any
  assert.equal(toolBlock2.result, 'ok')
  assert.equal(toolBlock2.isError, false)
})

test('applyRpcNotification clears pending permission on permission_resolved and marks cancelled tools', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withTool = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'tool_use', id: 't-exit', name: 'ExitSpecMode', input: { plan: 'p' } } },
  } as any)

  const withPerm = applyRpcRequest(withTool, sid, {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: 'r1',
    method: 'droid.request_permission',
    params: {
      toolUses: [{ toolUse: { id: 't-exit', name: 'ExitSpecMode', input: { plan: 'p' } } }],
      options: ['proceed_once', 'cancel'],
    },
  } as any)

  const resolved = applyRpcNotification(withPerm, sid, {
    ...baseNotif,
    params: { notification: { type: 'permission_resolved', requestId: 'r1', toolUseIds: ['t-exit'], selectedOption: 'cancel' } },
  } as any)

  const buf = resolved.get(sid)!
  assert.equal(buf.pendingPermissionRequests?.length || 0, 0)
  const toolBlock = buf.messages.flatMap((m) => m.blocks).find((b: any) => b.kind === 'tool_call' && b.callId === 't-exit') as any
  assert.ok(toolBlock)
  assert.equal(toolBlock.result, 'Cancelled')
  assert.equal(toolBlock.isError, true)
})

test('applyRpcNotification extracts tool_use from create_message content', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])
  const next = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'create_message',
        message: {
          id: 'm2',
          role: 'assistant',
          content: [
            { type: 'text', text: 'running tool' },
            { type: 'tool_use', id: 't2', name: 'Execute', input: { command: 'pwd' } },
          ],
        },
      },
    },
  } as any)

  const buf = next.get(sid)!
  assert.equal(buf.messages.length, 1)
  const msg = buf.messages[0]
  assert.equal(msg.id, 'droid:m2')
  assert.equal((msg.blocks[0] as any).kind, 'text')
  assert.equal((msg.blocks[0] as any).content, 'running tool')
  const toolBlock = msg.blocks.find((b) => b.kind === 'tool_call') as any
  assert.ok(toolBlock)
  assert.equal(toolBlock.callId, 't2')
  assert.equal(toolBlock.toolName, 'Execute')
  assert.equal(toolBlock.parameters.command, 'pwd')
})

test('applyRpcNotification does not duplicate assistant text when deltas land in blocks[1] then create_message arrives', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withDelta = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'assistant_text_delta', messageId: 'm1', blockIndex: 1, textDelta: 'Hello' } },
  } as any)

  const withSnapshot = applyRpcNotification(withDelta, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'create_message',
        message: { id: 'm1', role: 'assistant', content: [{ type: 'text', text: 'Hello' }] },
      },
    },
  } as any)

  const buf = withSnapshot.get(sid)!
  assert.equal(buf.messages.length, 1)
  const msg = buf.messages[0]
  const nonEmptyTextBlocks = msg.blocks.filter((b: any) => b.kind === 'text' && String(b.content || '').trim().length > 0) as any[]
  assert.equal(nonEmptyTextBlocks.length, 1)
  assert.equal(nonEmptyTextBlocks[0].content, 'Hello')
})

test('applyRpcNotification appends assistant_text_delta to existing snapshot text when delta blockIndex differs', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withSnapshot = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'create_message',
        message: { id: 'm1', role: 'assistant', content: [{ type: 'text', text: 'Hello' }] },
      },
    },
  } as any)

  const withDelta = applyRpcNotification(withSnapshot, sid, {
    ...baseNotif,
    params: { notification: { type: 'assistant_text_delta', messageId: 'm1', blockIndex: 1, textDelta: ' world' } },
  } as any)

  const buf = withDelta.get(sid)!
  assert.equal(buf.messages.length, 1)
  const msg = buf.messages[0]
  const nonEmptyTextBlocks = msg.blocks.filter((b: any) => b.kind === 'text' && String(b.content || '').trim().length > 0) as any[]
  assert.equal(nonEmptyTextBlocks.length, 1)
  assert.equal(nonEmptyTextBlocks[0].content, 'Hello world')
})

test('applyRpcNotification dedupes repeated tool_use notifications by id', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const next1 = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'tool_use', id: 't1', name: 'Execute', input: { command: 'ls' } } },
  } as any)
  const next2 = applyRpcNotification(next1, sid, {
    ...baseNotif,
    params: { notification: { type: 'tool_use', id: 't1', name: 'Execute', input: { command: 'ls' } } },
  } as any)

  const buf = next2.get(sid)!
  const toolCalls = buf.messages.flatMap((m) => m.blocks).filter((b: any) => b.kind === 'tool_call' && b.callId === 't1')
  assert.equal(toolCalls.length, 1)
})

test('applyRpcNotification creates fallback tool block on tool_result without prior tool_use', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])
  const next = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'tool_result', toolUseId: 't-missing', content: 'done', isError: false } },
  } as any)

  const buf = next.get(sid)!
  assert.equal(buf.messages.length, 1)
  const toolBlock = buf.messages[0].blocks[0] as any
  assert.equal(toolBlock.kind, 'tool_call')
  assert.equal(toolBlock.callId, 't-missing')
  assert.equal(toolBlock.result, 'done')
  assert.equal(toolBlock.isError, false)
})

test('applyRpcRequest enqueues permission and ask_user requests', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withPerm = applyRpcRequest(prev, sid, {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: 'r1',
    method: 'droid.request_permission',
    params: { toolUses: [{ toolUse: { id: 't1' } }], options: [{ value: 'proceed_once' }] },
  } as any)
  const buf1 = withPerm.get(sid)!
  assert.equal(buf1.pendingPermissionRequests?.length, 1)
  assert.equal(buf1.pendingPermissionRequests?.[0].requestId, 'r1')

  const withAsk = applyRpcRequest(withPerm, sid, {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: 'r2',
    method: 'droid.ask_user',
    params: { toolCallId: 'c1', questions: [{ index: 0, question: 'Q?', options: ['A'] }] },
  } as any)
  const buf2 = withAsk.get(sid)!
  assert.equal(buf2.pendingAskUserRequests?.length, 1)
  assert.equal(buf2.pendingAskUserRequests?.[0].requestId, 'r2')
  assert.equal(buf2.pendingAskUserRequests?.[0].questions[0].question, 'Q?')
})

test('applyRpcRequest parses permission options when backend sends string array', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const next = applyRpcRequest(prev, sid, {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: 'r1',
    method: 'droid.request_permission',
    params: {
      toolUses: [{ toolUse: { id: 't1', name: 'ExitSpecMode' } }],
      options: ['proceed_once', 'proceed_auto_run_medium', 'cancel'],
    },
  } as any)

  const req = next.get(sid)?.pendingPermissionRequests?.[0]
  assert.ok(req)
  assert.deepEqual(req.options, ['proceed_once', 'proceed_auto_run_medium', 'cancel'])
})

test('applyRpcNotification maps working_state_changed to isRunning for both idle and non-idle states', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const running = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: { notification: { type: 'droid_working_state_changed', newState: 'executing_tool' } },
  } as any)
  assert.equal(running.get(sid)!.isRunning, true)

  const idle = applyRpcNotification(running, sid, {
    ...baseNotif,
    params: { notification: { type: 'droid_working_state_changed', newState: 'idle' } },
  } as any)
  assert.equal(idle.get(sid)!.isRunning, false)
})

test('applyRpcNotification syncs settings_updated into session buffer fields', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const next = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'settings_updated',
        settings: {
          modelId: 'gpt-5.1',
          reasoningEffort: 'none',
          autonomyLevel: 'auto-high',
        },
      },
    },
  } as any)

  const buf = next.get(sid)!
  assert.equal(buf.model, 'gpt-5.1')
  assert.equal(buf.reasoningEffort, 'none')
  assert.equal(buf.autoLevel, 'high')
})

test('applyRpcNotification stores session_token_usage_changed and mcp notifications', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withTokens = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'session_token_usage_changed',
        sessionId: sid,
        tokenUsage: {
          inputTokens: 1,
          outputTokens: 2,
          cacheCreationTokens: 3,
          cacheReadTokens: 4,
          thinkingTokens: 5,
        },
      },
    },
  } as any)
  assert.deepEqual(withTokens.get(sid)!.tokenUsage, {
    inputTokens: 1,
    outputTokens: 2,
    cacheCreationTokens: 3,
    cacheReadTokens: 4,
    thinkingTokens: 5,
  })

  const withMcp = applyRpcNotification(withTokens, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'mcp_status_changed',
        servers: [{ name: 'linear', status: 'connecting' }],
      },
    },
  } as any)
  assert.equal(Array.isArray(withMcp.get(sid)!.mcpServers), true)
  assert.equal((withMcp.get(sid)!.mcpServers as any[])?.length, 1)

  const withAuth = applyRpcNotification(withMcp, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'mcp_auth_required',
        serverName: 'linear',
        authUrl: 'https://example.com/auth',
      },
    },
  } as any)
  assert.deepEqual(withAuth.get(sid)!.mcpAuthRequired, { serverName: 'linear', authUrl: 'https://example.com/auth' })
})
