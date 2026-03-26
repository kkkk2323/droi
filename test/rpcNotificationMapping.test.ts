import test from 'node:test'
import assert from 'node:assert/strict'
import { makeBuffer, applyRpcNotification, applyRpcRequest } from '../src/renderer/src/state/appReducer.ts'
import { buildNotificationTraceInfo, formatNotificationTrace } from '../src/renderer/src/lib/notificationFingerprint.ts'
import {
  applyMissionDirSnapshot,
  applyMissionLoadSnapshot,
} from '../src/renderer/src/state/missionState.ts'

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

test('applyRpcNotification captures missionDir from ProposeMission tool_result notifications', () => {
  const sid = 's1'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withTool = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'tool_use',
        id: 't-mission',
        name: 'ProposeMission',
        input: { objective: 'Ship the feature' },
      },
    },
  } as any)

  const next = applyRpcNotification(withTool, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'tool_result',
        toolUseId: 't-mission',
        content: {
          summary: 'Mission proposed',
          missionDir: '/Users/clive/.factory/missions/base-session-123',
        },
        isError: false,
      },
    },
  } as any)

  const buf = next.get(sid)!
  assert.equal(buf.missionDir, '/Users/clive/.factory/missions/base-session-123')
  const toolBlock = buf.messages[0].blocks[0] as any
  assert.equal(toolBlock.result.includes('Mission proposed'), true)
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

test('applyRpcRequest preserves mission permission metadata for propose_mission and start_mission_run', () => {
  const sid = 'mission-permissions'
  const prev = new Map([[sid, makeBuffer('/repo')]])

  const withProposalPermission = applyRpcRequest(prev, sid, {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: 'r-mission-proposal',
    method: 'droid.request_permission',
    params: {
      confirmationType: 'propose_mission',
      toolUses: [{ toolUse: { id: 't-propose', name: 'ProposeMission' } }],
      options: ['proceed_once', 'cancel'],
    },
  } as any)

  const proposalRequest = withProposalPermission.get(sid)?.pendingPermissionRequests?.[0]
  assert.ok(proposalRequest)
  assert.equal(proposalRequest.confirmationType, 'propose_mission')

  const withRunPermission = applyRpcRequest(withProposalPermission, sid, {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: 'r-start-mission-run',
    method: 'droid.request_permission',
    params: {
      confirmationType: 'start_mission_run',
      toolUses: [{ toolUse: { id: 't-run', name: 'StartMissionRun' } }],
      options: ['proceed_once', 'cancel'],
    },
  } as any)

  const runRequest = withRunPermission.get(sid)?.pendingPermissionRequests?.[1]
  assert.ok(runRequest)
  assert.equal(runRequest.confirmationType, 'start_mission_run')
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
  assert.equal((buf as any).interactionMode, 'auto')
  assert.equal((buf as any).autonomyLevel, 'high')
})

test('applyRpcNotification replaces stale normal-session protocol fields from settings_updated', () => {
  const sid = 'normal-1'
  const prev = new Map([
    [
      sid,
      {
        ...makeBuffer('/repo'),
        autoLevel: 'medium',
        interactionMode: 'auto',
        autonomyLevel: 'medium',
      },
    ],
  ])

  const next = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'settings_updated',
        settings: {
          interactionMode: 'spec',
          autonomyLevel: 'off',
        },
      },
    },
  } as any)

  const buf = next.get(sid)!
  assert.equal(buf.autoLevel, 'default')
  assert.equal((buf as any).interactionMode, 'spec')
  assert.equal((buf as any).autonomyLevel, 'off')
})

test('applyRpcNotification does not downgrade explicit mission settings on generic settings_updated', () => {
  const sid = 'mission-1'
  const prev = new Map([
    [
      sid,
      {
        ...makeBuffer('/repo'),
        autoLevel: 'high',
        isMission: true,
        sessionKind: 'mission',
        interactionMode: 'agi',
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
      },
    ],
  ])

  const next = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'settings_updated',
        settings: {
          modelId: 'gpt-5.1',
          reasoningEffort: 'none',
          interactionMode: 'spec',
          autonomyLevel: 'off',
        },
      },
    },
  } as any)

  const buf = next.get(sid)!
  assert.equal(buf.model, 'gpt-5.1')
  assert.equal(buf.reasoningEffort, 'none')
  assert.equal(buf.autoLevel, 'high')
  assert.equal((buf as any).isMission, true)
  assert.equal((buf as any).sessionKind, 'mission')
  assert.equal((buf as any).interactionMode, 'agi')
  assert.equal((buf as any).autonomyLevel, 'high')
  assert.equal((buf as any).decompSessionType, 'orchestrator')
})

test('applyRpcNotification keeps StartMissionRun progress supplemental while Mission notifications stay authoritative', () => {
  const sid = 'mission-progress'
  const prev = new Map([
    [
      sid,
      {
        ...makeBuffer('/repo'),
        isMission: true,
        sessionKind: 'mission',
        interactionMode: 'agi',
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        mission: applyMissionLoadSnapshot(undefined, {
          state: {
            state: 'running',
            currentFeatureId: 'feature-live',
            currentWorkerSessionId: 'worker-load',
            completedFeatures: 0,
            totalFeatures: 2,
            updatedAt: '2026-03-09T00:00:00.000Z',
          },
          features: [
            { id: 'feature-live', status: 'in_progress' },
            { id: 'feature-next', status: 'pending' },
          ],
        }),
      },
    ],
  ])

  const withProgress = applyRpcNotification(prev, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'tool_progress_update',
        toolUseId: 'run-1',
        toolName: 'StartMissionRun',
        update: {
          missionState: 'completed',
          currentFeatureId: 'wrong-feature',
          currentWorkerSessionId: 'worker-hint',
          completedFeatures: 2,
          totalFeatures: 2,
        },
      },
    },
  } as any)

  const hintedMission = withProgress.get(sid)!.mission!
  assert.equal(hintedMission.currentState, 'running')
  assert.equal(hintedMission.currentFeatureId, 'feature-live')
  assert.equal(hintedMission.currentWorkerSessionId, 'worker-load')
  assert.equal(hintedMission.isCompleted, false)
  assert.equal(hintedMission.supplemental?.missionState, 'completed')
  assert.equal(hintedMission.supplemental?.currentWorkerSessionId, 'worker-hint')

  const withWorkerStarted = applyRpcNotification(withProgress, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'mission_worker_started',
        workerSessionId: 'worker-live',
        featureId: 'feature-live',
      },
    },
  } as any)

  const liveMission = withWorkerStarted.get(sid)!.mission!
  assert.equal(liveMission.currentWorkerSessionId, 'worker-live')
  assert.equal(liveMission.liveWorkerSessionId, 'worker-live')

  const withWorkerCompleted = applyRpcNotification(withWorkerStarted, sid, {
    ...baseNotif,
    params: {
      notification: {
        type: 'mission_worker_completed',
        workerSessionId: 'worker-live',
        featureId: 'feature-live',
        successState: 'success',
        handoffFileName: 'feature-live.json',
        handoff: {
          salientSummary: 'Finished feature-live.',
          whatWasImplemented: 'Completed the feature-live worker and recorded verification.',
        },
      },
    },
  } as any)

  const completedMission = withWorkerCompleted.get(sid)!.mission!
  assert.equal(completedMission.liveWorkerSessionId, undefined)
  assert.equal(completedMission.currentWorkerSessionId, undefined)
  assert.equal(completedMission.isCompleted, false)
  assert.equal(completedMission.handoffs.length, 1)
  assert.equal(completedMission.handoffs[0]?.fileName, 'feature-live.json')
  assert.equal((completedMission.handoffs[0]?.payload as any)?.featureId, 'feature-live')
  assert.equal((completedMission.handoffs[0]?.payload as any)?.successState, 'success')
  assert.equal(
    (((completedMission.handoffs[0]?.payload as any)?.handoff as any)?.salientSummary as string) || '',
    'Finished feature-live.',
  )
})

test('applyMissionDirSnapshot follows per-file merge rules and keeps validator-injected Missions running', () => {
  const loadMission = applyMissionLoadSnapshot(undefined, {
    state: {
      state: 'running',
      currentFeatureId: 'mission-protocol-metadata-and-kill-worker-plumbing',
      completedFeatures: 1,
      totalFeatures: 2,
      updatedAt: '2026-03-09T01:30:00.000Z',
    },
    features: [
      { id: 'mission-protocol-metadata-and-kill-worker-plumbing', status: 'completed' },
      { id: 'mission-store-restore-and-reconciliation', status: 'in_progress' },
    ],
    progressLog: [
      { timestamp: '2026-03-09T01:29:00.000Z', type: 'worker_completed', featureId: 'mission-protocol-metadata-and-kill-worker-plumbing' },
    ],
    handoffs: {
      'handoff-1.json': {
        featureId: 'mission-protocol-metadata-and-kill-worker-plumbing',
        successState: 'success',
      },
    },
  })!

  const reconciled = applyMissionDirSnapshot(loadMission, {
    missionDir: '/Users/clive/.factory/missions/base-session-123',
    exists: true,
    state: {
      state: 'running',
      currentFeatureId: 'scrutiny-validator-mission-foundation',
      currentWorkerSessionId: 'validator-worker',
      completedFeatures: 1,
      totalFeatures: 4,
      updatedAt: '2026-03-09T02:07:12.000Z',
      milestonesWithValidationPlanned: ['mission-foundation'],
    },
    features: [
      { id: 'mission-protocol-metadata-and-kill-worker-plumbing', status: 'completed' },
      { id: 'mission-session-entry-routing-and-sidebar', status: 'completed' },
      {
        id: 'scrutiny-validator-mission-foundation',
        status: 'in_progress',
        skillName: 'scrutiny-validator',
      },
      {
        id: 'user-testing-validator-mission-foundation',
        status: 'pending',
        skillName: 'user-testing-validator',
      },
    ],
    progressEntries: [
      { timestamp: '2026-03-09T01:29:00.000Z', type: 'worker_completed', featureId: 'mission-protocol-metadata-and-kill-worker-plumbing' },
      { timestamp: '2026-03-09T02:07:12.035Z', type: 'milestone_validation_triggered', milestone: 'mission-foundation' },
    ],
    handoffs: [
      {
        fileName: 'handoff-2.json',
        payload: { featureId: 'mission-session-entry-routing-and-sidebar', successState: 'success' },
      },
    ],
    validationState: {
      assertions: {
        'VAL-SESSION-001': { status: 'passed' },
        'VAL-RECOVERY-001': { status: 'pending' },
      },
    },
  })

  assert.equal(reconciled.currentFeatureId, 'scrutiny-validator-mission-foundation')
  assert.equal(reconciled.totalFeatures, 4)
  assert.equal(reconciled.completedFeatures, 2)
  assert.equal(reconciled.isCompleted, false)
  assert.equal(reconciled.progressEntries.length, 2)
  assert.equal(reconciled.handoffs.length, 2)
  assert.equal(reconciled.liveWorkerSessionId, undefined)
})

test('applyMissionDirSnapshot supports disk-only recovery, completion gating, and orphan worker recovery', () => {
  const recovered = applyMissionDirSnapshot(undefined, {
    missionDir: '/Users/clive/.factory/missions/base-session-456',
    exists: true,
    state: {
      state: 'completed',
      currentWorkerSessionId: 'orphan-worker',
      completedFeatures: 2,
      totalFeatures: 2,
      updatedAt: '2026-03-09T03:00:00.000Z',
      milestonesWithValidationPlanned: ['mission-sync-recovery'],
    },
    features: [
      { id: 'feature-a', status: 'completed' },
      { id: 'feature-b', status: 'completed' },
    ],
    progressEntries: [{ timestamp: '2026-03-09T02:59:00.000Z', type: 'worker_completed' }],
    handoffs: [
      { fileName: 'feature-a.json', payload: { featureId: 'feature-a', successState: 'success' } },
    ],
    validationState: {
      assertions: {
        'VAL-RECOVERY-001': { status: 'pending' },
        'VAL-RECOVERY-002': { status: 'pending' },
      },
    },
  })

  assert.equal(recovered.currentState, 'completed')
  assert.equal(recovered.currentWorkerSessionId, undefined)
  assert.equal(recovered.liveWorkerSessionId, undefined)
  assert.equal(recovered.isCompleted, false)

  const finalized = applyMissionDirSnapshot(recovered, {
    missionDir: '/Users/clive/.factory/missions/base-session-456',
    exists: true,
    state: {
      state: 'completed',
      completedFeatures: 2,
      totalFeatures: 2,
      updatedAt: '2026-03-09T03:05:00.000Z',
      milestonesWithValidationPlanned: ['mission-sync-recovery'],
    },
    features: [
      { id: 'feature-a', status: 'completed' },
      { id: 'feature-b', status: 'completed' },
    ],
    progressEntries: [{ timestamp: '2026-03-09T03:05:01.000Z', type: 'mission_completed' }],
    handoffs: [],
    validationState: {
      assertions: {
        'VAL-RECOVERY-001': { status: 'passed' },
        'VAL-RECOVERY-002': { status: 'passed' },
      },
    },
  })

  assert.equal(finalized.isCompleted, true)
  assert.equal(finalized.progressEntries.length, 2)
  assert.equal(finalized.handoffs.length, 1)
})

test('applyMissionDirSnapshot clears stale paused state when newer progress shows the worker finished', () => {
  const reconciled = applyMissionDirSnapshot(undefined, {
    missionDir: '/Users/clive/.factory/missions/base-session-789',
    exists: true,
    state: {
      state: 'paused',
      currentFeatureId: 'user-testing-validator-runtime',
      currentWorkerSessionId: 'worker-paused',
      pausedWorkerSessionId: 'worker-paused',
      completedFeatures: 6,
      totalFeatures: 7,
      updatedAt: '2026-03-11T09:39:08.773Z',
    },
    features: [
      { id: 'feature-a', status: 'completed' },
      { id: 'feature-b', status: 'completed' },
      { id: 'feature-c', status: 'completed' },
      { id: 'feature-d', status: 'completed' },
      { id: 'feature-e', status: 'completed' },
      { id: 'feature-f', status: 'completed' },
      { id: 'user-testing-validator-runtime', status: 'completed' },
      { id: 'feature-g', status: 'pending' },
    ],
    progressEntries: [
      {
        timestamp: '2026-03-11T09:39:08.763Z',
        type: 'worker_paused',
        workerSessionId: 'worker-paused',
        featureId: 'user-testing-validator-runtime',
      },
      {
        timestamp: '2026-03-11T09:39:08.773Z',
        type: 'mission_paused',
      },
      {
        timestamp: '2026-03-11T11:17:21.544Z',
        type: 'worker_completed',
        workerSessionId: 'worker-paused',
        featureId: 'user-testing-validator-runtime',
      },
    ],
    handoffs: [],
    validationState: null,
  })

  assert.equal(reconciled.currentState, 'orchestrator_turn')
  assert.equal(reconciled.currentWorkerSessionId, undefined)
  assert.equal(reconciled.liveWorkerSessionId, undefined)
  assert.equal(reconciled.pausedWorkerSessionId, undefined)
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
