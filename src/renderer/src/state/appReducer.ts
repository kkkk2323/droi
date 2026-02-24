import type {
  ChatMessage,
  ToolCallBlock,
  JsonRpcNotification,
  JsonRpcRequest,
  DroidPermissionOption,
  WorkspaceType,
  SetupScriptEvent,
  SetupScriptStatus,
} from '@/types'
import { isTraceChainEnabled } from '../lib/notificationFingerprint.ts'
import { uuidv4 } from '../lib/uuid.ts'

export interface SessionBuffer {
  messages: ChatMessage[]
  isRunning: boolean
  isCancelling: boolean
  isSetupRunning: boolean
  apiKeyFingerprint?: string
  pendingApiKeyFingerprint?: string
  projectDir: string
  repoRoot?: string
  branch?: string
  workspaceType?: WorkspaceType
  baseBranch?: string
  model: string
  autoLevel: string
  reasoningEffort: string
  tokenUsage?: {
    inputTokens: number
    outputTokens: number
    cacheCreationTokens: number
    cacheReadTokens: number
    thinkingTokens: number
  }
  mcpServers?: unknown[]
  mcpAuthRequired?: { serverName: string; authUrl: string } | null
  settingsFlashAt?: number
  pendingSendMessageIds: Record<string, true>
  pendingPermissionRequests?: PendingPermissionRequest[]
  pendingAskUserRequests?: PendingAskUserRequest[]
  debugTrace?: string[]
  setupScript: SessionSetupState
}

export interface SessionSetupState {
  script: string
  status: SetupScriptStatus
  output: string
  error?: string
  exitCode: number | null
}

export const DEFAULT_MODEL = 'kimi-k2.5'
export const DEFAULT_AUTO_LEVEL = 'default'
const MAX_SETUP_OUTPUT_CHARS = 120_000

export function makeBuffer(
  projectDir: string,
  workspace?: {
    repoRoot?: string
    branch?: string
    workspaceType?: WorkspaceType
    baseBranch?: string
  },
): SessionBuffer {
  return {
    messages: [],
    isRunning: false,
    isCancelling: false,
    isSetupRunning: false,
    apiKeyFingerprint: undefined,
    pendingApiKeyFingerprint: undefined,
    projectDir,
    repoRoot: workspace?.repoRoot,
    branch: workspace?.branch,
    workspaceType: workspace?.workspaceType,
    baseBranch: workspace?.baseBranch,
    model: DEFAULT_MODEL,
    autoLevel: DEFAULT_AUTO_LEVEL,
    reasoningEffort: '',
    tokenUsage: undefined,
    mcpServers: undefined,
    mcpAuthRequired: null,
    settingsFlashAt: undefined,
    pendingSendMessageIds: {},
    pendingPermissionRequests: [],
    pendingAskUserRequests: [],
    debugTrace: [],
    setupScript: {
      script: '',
      status: 'idle',
      output: '',
      exitCode: null,
    },
  }
}

function mapSettingsToAutoLevel(settings: Record<string, unknown>): string | null {
  const interactionMode =
    typeof (settings as any).interactionMode === 'string'
      ? String((settings as any).interactionMode)
          .trim()
          .toLowerCase()
      : ''
  if (interactionMode === 'spec') return 'default'

  const autonomyLevel =
    typeof (settings as any).autonomyLevel === 'string'
      ? String((settings as any).autonomyLevel)
          .trim()
          .toLowerCase()
      : ''
  if (autonomyLevel === 'low') return 'low'
  if (autonomyLevel === 'medium') return 'medium'
  if (autonomyLevel === 'high') return 'high'

  // Best-effort parsing for older payloads.
  if (autonomyLevel === 'auto-low') return 'low'
  if (autonomyLevel === 'auto-medium') return 'medium'
  if (autonomyLevel === 'auto-high') return 'high'
  if (autonomyLevel === 'spec' || autonomyLevel === 'normal') return 'default'

  const autonomyMode =
    typeof (settings as any).autonomyMode === 'string'
      ? String((settings as any).autonomyMode)
          .trim()
          .toLowerCase()
      : ''
  if (autonomyMode === 'auto-low') return 'low'
  if (autonomyMode === 'auto-medium') return 'medium'
  if (autonomyMode === 'auto-high') return 'high'

  return null
}

function updateSessionMessages(
  prev: Map<string, SessionBuffer>,
  sid: string,
  updater: (msgs: ChatMessage[]) => ChatMessage[],
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev
  const next = new Map(prev)
  next.set(sid, { ...session, messages: updater(session.messages) })
  return next
}

export interface PermissionOptionMeta {
  value: DroidPermissionOption
  label: string
  selectedColor?: string
  selectedPrefix?: string
}

export interface PendingPermissionRequest {
  requestId: string
  toolUses: unknown[]
  options: DroidPermissionOption[]
  optionsMeta: PermissionOptionMeta[]
  raw: JsonRpcRequest
}

export interface AskUserQuestion {
  index: number
  topic?: string
  question: string
  options: string[]
}

export interface PendingAskUserRequest {
  requestId: string
  toolCallId: string
  questions: AskUserQuestion[]
  raw: JsonRpcRequest
}

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function formatUnknown(value: unknown): string {
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function findOrCreateAssistantMessage(
  msgs: ChatMessage[],
  droidMessageId: string,
  timestamp: number,
): { msgs: ChatMessage[]; idx: number } {
  const id = `droid:${droidMessageId}`
  const existingIdx = msgs.findIndex((m) => m.id === id)
  if (existingIdx !== -1) return { msgs, idx: existingIdx }
  const next: ChatMessage = {
    id,
    role: 'assistant',
    blocks: [{ kind: 'text', content: '' }],
    timestamp,
  }
  return { msgs: [...msgs, next], idx: msgs.length }
}

function ensureTextBlock(msg: ChatMessage, blockIndex: number): ChatMessage {
  const blocks = [...msg.blocks]
  for (let i = blocks.length; i <= blockIndex; i++) blocks.push({ kind: 'text', content: '' })
  const existing = blocks[blockIndex]
  if (existing.kind !== 'text') blocks[blockIndex] = { kind: 'text', content: '' }
  return { ...msg, blocks }
}

function appendAssistantTextDelta(
  msg: ChatMessage,
  blockIndex: number,
  textDelta: string,
): ChatMessage {
  const ensured = ensureTextBlock(msg, blockIndex)
  const blocks = [...ensured.blocks]
  const b = blocks[blockIndex] as any
  blocks[blockIndex] = { kind: 'text', content: String(b.content || '') + textDelta }
  return { ...ensured, blocks }
}

function addToolUse(
  msgs: ChatMessage[],
  toolUse: { id: string; name: string; input: Record<string, unknown> },
  timestamp: number,
): ChatMessage[] {
  const block: ToolCallBlock = {
    kind: 'tool_call',
    callId: toolUse.id,
    toolName: toolUse.name,
    parameters: toolUse.input,
  }
  const last = msgs[msgs.length - 1]
  if (last && last.role === 'assistant') {
    return [...msgs.slice(0, -1), { ...last, blocks: [...last.blocks, block] }]
  }
  return [...msgs, { id: uuidv4(), role: 'assistant', blocks: [block], timestamp }]
}

function updateToolCall(
  msgs: ChatMessage[],
  callId: string,
  updater: (block: ToolCallBlock) => ToolCallBlock,
): ChatMessage[] {
  const updated = [...msgs]
  for (let i = updated.length - 1; i >= 0; i--) {
    const msg = updated[i]
    if (msg.role !== 'assistant') continue
    const idx = msg.blocks.findIndex((b) => b.kind === 'tool_call' && b.callId === callId)
    if (idx === -1) continue
    const block = msg.blocks[idx] as ToolCallBlock
    const blocks = [...msg.blocks]
    blocks[idx] = updater(block)
    updated[i] = { ...msg, blocks }
    return updated
  }
  return msgs
}

type ParsedToolUse = {
  id: string
  name: string
  input: Record<string, unknown>
}

function extractMessageContent(content: unknown): { text: string; toolUses: ParsedToolUse[] } {
  if (!Array.isArray(content)) return { text: '', toolUses: [] }
  const parts: string[] = []
  const toolUses: ParsedToolUse[] = []
  for (const item of content) {
    if (!item || typeof item !== 'object') continue
    const t = (item as any).type
    if (t === 'text' && typeof (item as any).text === 'string') parts.push((item as any).text)
    if (t === 'tool_use') {
      const id = String((item as any).id || '')
      const name = String((item as any).name || '')
      const input = (item as any).input
      if (id && name && isObject(input)) {
        toolUses.push({ id, name, input: input as Record<string, unknown> })
      }
    }
  }
  return { text: parts.join(''), toolUses }
}

function getNonEmptyTextBlockIndexes(msg: ChatMessage): number[] {
  const idxs: number[] = []
  for (let i = 0; i < msg.blocks.length; i++) {
    const b = msg.blocks[i]
    if (b?.kind !== 'text') continue
    if (String((b as any).content || '').trim() === '') continue
    idxs.push(i)
  }
  return idxs
}

function getSingleNonEmptyTextBlockIndex(msg: ChatMessage): number | null {
  const idxs = getNonEmptyTextBlockIndexes(msg)
  return idxs.length === 1 ? idxs[0] : null
}

function withCreateMessageText(existing: ChatMessage, text: string): ChatMessage {
  if (!text) return existing

  // If we already have streamed assistant text in any block (e.g. deltas landed in blocks[1]),
  // write the snapshot into that block and clear any other non-empty text blocks to avoid
  // rendering duplicated paragraphs.
  const nonEmptyTextIdxs = getNonEmptyTextBlockIndexes(existing)
  if (nonEmptyTextIdxs.length > 0) {
    const targetIdx = nonEmptyTextIdxs[nonEmptyTextIdxs.length - 1]
    const blocks = [...existing.blocks]
    let changed = false

    const target = blocks[targetIdx]
    if (target?.kind === 'text' && target.content !== text) {
      blocks[targetIdx] = { kind: 'text', content: text }
      changed = true
    }

    for (const idx of nonEmptyTextIdxs) {
      if (idx === targetIdx) continue
      const b = blocks[idx]
      if (b?.kind !== 'text') continue
      if (String(b.content || '').trim() === '') continue
      blocks[idx] = { kind: 'text', content: '' }
      changed = true
    }

    return changed ? { ...existing, blocks } : existing
  }

  // No non-empty text blocks yet: default to blocks[0] (keeps snapshot-first flows working).
  if (existing.blocks.length === 0)
    return { ...existing, blocks: [{ kind: 'text', content: text }] }
  if (existing.blocks[0]?.kind !== 'text') return existing
  const first = existing.blocks[0]
  if (String(first.content || '').trim() !== '') return existing
  const blocks = [...existing.blocks]
  blocks[0] = { kind: 'text', content: text }
  return { ...existing, blocks }
}

function withMissingToolUses(existing: ChatMessage, toolUses: ParsedToolUse[]): ChatMessage {
  if (toolUses.length === 0) return existing
  const existingCallIds = new Set(
    existing.blocks.filter((b): b is ToolCallBlock => b.kind === 'tool_call').map((b) => b.callId),
  )
  const toAppend: ToolCallBlock[] = toolUses
    .filter((toolUse) => !existingCallIds.has(toolUse.id))
    .map((toolUse) => ({
      kind: 'tool_call',
      callId: toolUse.id,
      toolName: toolUse.name,
      parameters: toolUse.input,
    }))
  if (toAppend.length === 0) return existing
  return { ...existing, blocks: [...existing.blocks, ...toAppend] }
}

function appendToolCallFallback(
  msgs: ChatMessage[],
  now: number,
  block: ToolCallBlock,
): ChatMessage[] {
  const last = msgs[msgs.length - 1]
  if (last && last.role === 'assistant') {
    return [...msgs.slice(0, -1), { ...last, blocks: [...last.blocks, block] }]
  }
  return [...msgs, { id: uuidv4(), role: 'assistant', blocks: [block], timestamp: now }]
}

export function applyRpcNotification(
  prev: Map<string, SessionBuffer>,
  sid: string,
  message: JsonRpcNotification,
): Map<string, SessionBuffer> {
  if (message.method !== 'droid.session_notification') return prev
  const params = message.params
  const notification = isObject(params) ? (params as any).notification : null
  if (!notification || !isObject(notification) || typeof (notification as any).type !== 'string')
    return prev

  const type = String((notification as any).type)
  const now = Date.now()

  if (type === 'assistant_text_delta') {
    const messageId = String((notification as any).messageId || '')
    const blockIndex = Number((notification as any).blockIndex || 0)
    const textDelta = String((notification as any).textDelta || '')
    if (!messageId || !textDelta) return prev
    return updateSessionMessages(prev, sid, (msgs) => {
      const { msgs: withMsg, idx } = findOrCreateAssistantMessage(msgs, messageId, now)
      const m = withMsg[idx]
      const safeBlockIndex = Number.isFinite(blockIndex) ? blockIndex : 0
      const singleNonEmptyIdx = getSingleNonEmptyTextBlockIndex(m)
      const effectiveBlockIndex = singleNonEmptyIdx !== null ? singleNonEmptyIdx : safeBlockIndex
      const updated = appendAssistantTextDelta(m, effectiveBlockIndex, textDelta)
      const out = [...withMsg]
      out[idx] = updated
      return out
    })
  }

  if (type === 'thinking_text_delta') {
    const messageId = String((notification as any).messageId || '')
    const textDelta = String((notification as any).textDelta || '')
    if (!messageId || !textDelta) return prev
    return updateSessionMessages(prev, sid, (msgs) => {
      const { msgs: withMsg, idx } = findOrCreateAssistantMessage(msgs, messageId, now)
      const m = withMsg[idx]
      const blocks = [...m.blocks]
      const thinkingIdx = blocks.findIndex((b) => b.kind === 'thinking')
      if (thinkingIdx !== -1) {
        const existing = blocks[thinkingIdx] as any
        blocks[thinkingIdx] = {
          kind: 'thinking',
          content: String(existing.content || '') + textDelta,
        }
      } else {
        blocks.unshift({ kind: 'thinking', content: textDelta })
      }
      const out = [...withMsg]
      out[idx] = { ...m, blocks }
      return out
    })
  }

  if (type === 'create_message') {
    const msg = (notification as any).message
    if (!isObject(msg)) return prev
    const role = String((msg as any).role || '')
    const id = String((msg as any).id || '')
    if (role !== 'assistant' || !id) return prev
    const { text, toolUses } = extractMessageContent((msg as any).content)
    return updateSessionMessages(prev, sid, (msgs) => {
      const { msgs: withMsg, idx } = findOrCreateAssistantMessage(msgs, id, now)
      const existing = withMsg[idx]
      const withText = withCreateMessageText(existing, text)
      const withTools = withMissingToolUses(withText, toolUses)
      if (withTools === existing) return withMsg
      const out = [...withMsg]
      out[idx] = withTools
      return out
    })
  }

  if (type === 'tool_use') {
    const id = String((notification as any).id || '')
    const name = String((notification as any).name || '')
    const input = (notification as any).input
    if (!id || !name || !isObject(input)) return prev
    return updateSessionMessages(prev, sid, (msgs) => {
      // Avoid duplicated tool activity blocks if upstream repeats tool_use notifications.
      const updated = updateToolCall(msgs, id, (block) => {
        const hasParams = Boolean(block.parameters) && Object.keys(block.parameters).length > 0
        const nextName = block.toolName && block.toolName !== 'Tool' ? block.toolName : name
        const nextParams = hasParams ? block.parameters : (input as any)
        if (nextName === block.toolName && nextParams === block.parameters) return block
        return { ...block, toolName: nextName, parameters: nextParams }
      })
      if (updated !== msgs) return updated
      return addToolUse(msgs, { id, name, input: input as any }, now)
    })
  }

  if (type === 'tool_result') {
    const toolUseId = String((notification as any).toolUseId || '')
    if (!toolUseId) return prev
    const content = (notification as any).content
    const isError = Boolean((notification as any).isError)
    const rendered = content === undefined ? '' : formatUnknown(content)
    return updateSessionMessages(prev, sid, (msgs) => {
      const updated = updateToolCall(msgs, toolUseId, (block) => ({
        ...block,
        result: rendered,
        isError,
      }))
      if (updated !== msgs) return updated
      const toolName = String((notification as any).toolName || 'Tool')
      return appendToolCallFallback(updated, now, {
        kind: 'tool_call',
        callId: toolUseId,
        toolName,
        parameters: {},
        result: rendered,
        isError,
      })
    })
  }

  if (type === 'tool_progress_update') {
    const toolUseId = String((notification as any).toolUseId || '')
    if (!toolUseId) return prev
    const update = (notification as any).update
    const rendered = formatUnknown(update)
    return updateSessionMessages(prev, sid, (msgs) => {
      const updated = updateToolCall(msgs, toolUseId, (block) => ({ ...block, progress: rendered }))
      if (updated !== msgs) return updated
      const toolName = String((notification as any).toolName || 'Tool')
      return appendToolCallFallback(updated, now, {
        kind: 'tool_call',
        callId: toolUseId,
        toolName,
        parameters: {},
        progress: rendered,
      })
    })
  }

  if (type === 'permission_resolved') {
    const requestId = String((notification as any).requestId || '')
    const selectedOption = String((notification as any).selectedOption || '')
    const toolUseIdsRaw = (notification as any).toolUseIds
    const toolUseIds: string[] = Array.isArray(toolUseIdsRaw)
      ? toolUseIdsRaw.map((v: unknown) => String(v || '')).filter(Boolean)
      : []
    if (!requestId) return prev

    const session = prev.get(sid)
    if (!session) return prev

    const pending = session.pendingPermissionRequests || []
    const matching = pending.find((r) => r.requestId === requestId) || null
    const nameById = new Map<string, string>()
    if (matching && Array.isArray((matching as any).toolUses)) {
      for (const item of (matching as any).toolUses) {
        const raw = (item as any)?.toolUse || item
        if (!raw || typeof raw !== 'object') continue
        const id = String((raw as any).id || (raw as any).toolUseId || '')
        const name = String(
          (raw as any).name || (raw as any).toolName || (raw as any).recipient_name || '',
        )
        if (id && name) nameById.set(id, name)
      }
    }

    // Always clear the pending request from local state once the backend confirms resolution.
    const rest = pending.filter((r) => r.requestId !== requestId)
    let next = new Map(prev)
    next.set(sid, { ...session, pendingPermissionRequests: rest })

    // If the user cancelled, the tool(s) will never produce tool_result events.
    // Mark the tool blocks as cancelled so the UI doesn't show an infinite spinner.
    if (selectedOption === 'cancel' && toolUseIds.length > 0) {
      next = updateSessionMessages(next, sid, (msgs) => {
        let out = msgs
        for (const id of toolUseIds) {
          const before = out
          const updated = updateToolCall(out, id, (block) => {
            if (typeof block.result === 'string' && block.result.trim() !== '') return block
            return { ...block, result: 'Cancelled', isError: true }
          })
          out = updated
          // If the tool block doesn't exist yet, append a minimal fallback.
          if (updated === before) {
            // updateToolCall returns the same array if not found; detect by searching quickly.
            const found = out.some(
              (m) =>
                m.role === 'assistant' &&
                m.blocks.some((b) => b.kind === 'tool_call' && (b as any).callId === id),
            )
            if (!found) {
              out = appendToolCallFallback(out, now, {
                kind: 'tool_call',
                callId: id,
                toolName: nameById.get(id) || 'Tool',
                parameters: {},
                result: 'Cancelled',
                isError: true,
              })
            }
          }
        }
        return out
      })
    }

    return next
  }

  if (type === 'droid_working_state_changed' || type === 'working_state_changed') {
    const newState = String((notification as any).newState || '')
    const normalized = newState.trim().toLowerCase()
    if (!normalized) return prev
    const session = prev.get(sid)
    if (!session) return prev
    const next = new Map(prev)
    next.set(sid, { ...session, isRunning: normalized !== 'idle' })
    return next
  }

  if (type === 'error') {
    const msg = String((notification as any).message || 'Unknown error')
    const session = prev.get(sid)
    if (!session) return prev
    const next = new Map(prev)
    next.set(sid, {
      ...session,
      isRunning: false,
      messages: [
        ...session.messages,
        { id: uuidv4(), role: 'error', blocks: [{ kind: 'text', content: msg }], timestamp: now },
      ],
    })
    return next
  }

  if (type === 'settings_updated') {
    const settings = (notification as any).settings
    if (!isObject(settings)) return prev
    const modelId =
      typeof (settings as any).modelId === 'string' ? String((settings as any).modelId) : ''
    const reasoningEffort =
      typeof (settings as any).reasoningEffort === 'string'
        ? String((settings as any).reasoningEffort)
        : ''
    const nextAuto = mapSettingsToAutoLevel(settings)
    const session = prev.get(sid)
    if (!session) return prev
    const next = new Map(prev)
    const hasChange =
      (modelId && modelId !== session.model) ||
      (reasoningEffort && reasoningEffort !== session.reasoningEffort) ||
      (nextAuto && nextAuto !== session.autoLevel)
    next.set(sid, {
      ...session,
      ...(modelId ? { model: modelId } : {}),
      ...(reasoningEffort ? { reasoningEffort } : {}),
      ...(nextAuto ? { autoLevel: nextAuto } : {}),
      ...(hasChange ? { settingsFlashAt: Date.now() } : {}),
    })
    return next
  }

  if (type === 'session_token_usage_changed') {
    const tokenUsage = (notification as any).tokenUsage
    if (!isObject(tokenUsage)) return prev
    const session = prev.get(sid)
    if (!session) return prev
    const next = new Map(prev)
    next.set(sid, {
      ...session,
      tokenUsage: {
        inputTokens: Number((tokenUsage as any).inputTokens || 0),
        outputTokens: Number((tokenUsage as any).outputTokens || 0),
        cacheCreationTokens: Number((tokenUsage as any).cacheCreationTokens || 0),
        cacheReadTokens: Number((tokenUsage as any).cacheReadTokens || 0),
        thinkingTokens: Number((tokenUsage as any).thinkingTokens || 0),
      },
    })
    return next
  }

  if (type === 'mcp_status_changed') {
    const servers = (notification as any).servers
    if (!Array.isArray(servers)) return prev
    const session = prev.get(sid)
    if (!session) return prev
    const next = new Map(prev)
    next.set(sid, { ...session, mcpServers: servers })
    return next
  }

  if (type === 'mcp_auth_required') {
    const serverName = String((notification as any).serverName || '').trim()
    const authUrl = String((notification as any).authUrl || '').trim()
    if (!serverName || !authUrl) return prev
    const session = prev.get(sid)
    if (!session) return prev
    const next = new Map(prev)
    next.set(sid, { ...session, mcpAuthRequired: { serverName, authUrl } })
    return next
  }

  return prev
}

const DEFAULT_PERMISSION_OPTIONS: DroidPermissionOption[] = [
  'proceed_once',
  'proceed_always',
  'proceed_auto_run_low',
  'proceed_auto_run_medium',
  'proceed_auto_run_high',
  'cancel',
]

export function applyRpcRequest(
  prev: Map<string, SessionBuffer>,
  sid: string,
  message: JsonRpcRequest,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev

  if (message.method === 'droid.request_permission') {
    const params = message.params
    const toolUses =
      isObject(params) && Array.isArray((params as any).toolUses) ? (params as any).toolUses : []
    const options =
      isObject(params) && Array.isArray((params as any).options)
        ? ((params as any).options as any[])
            .map((o) => {
              if (typeof o === 'string') return o
              if (!o || typeof o !== 'object') return ''
              return String((o as any)?.value || (o as any)?.id || '')
            })
            .filter(Boolean)
        : []
    const normalizedOptions = (
      options.length ? options : DEFAULT_PERMISSION_OPTIONS
    ) as DroidPermissionOption[]

    const rawOptions =
      isObject(params) && Array.isArray((params as any).options)
        ? ((params as any).options as any[])
        : []
    const optionsMeta: PermissionOptionMeta[] = normalizedOptions.map((val) => {
      const raw = rawOptions.find(
        (o: any) => typeof o === 'object' && o && String(o.value || o.id || '') === val,
      )
      return {
        value: val,
        label: typeof raw?.label === 'string' ? raw.label : val,
        selectedColor: typeof raw?.selectedColor === 'string' ? raw.selectedColor : undefined,
        selectedPrefix: typeof raw?.selectedPrefix === 'string' ? raw.selectedPrefix : undefined,
      }
    })

    const req: PendingPermissionRequest = {
      requestId: message.id,
      toolUses,
      options: normalizedOptions,
      optionsMeta,
      raw: message,
    }
    const next = new Map(prev)
    next.set(sid, {
      ...session,
      pendingPermissionRequests: [...(session.pendingPermissionRequests || []), req],
    })
    return next
  }

  if (message.method === 'droid.ask_user') {
    const params = message.params
    const toolCallId = isObject(params) ? String((params as any).toolCallId || '') : ''
    const questionsRaw: unknown[] =
      isObject(params) && Array.isArray((params as any).questions)
        ? ((params as any).questions as unknown[])
        : []
    const questions: AskUserQuestion[] = questionsRaw
      .map((item, idx: number): AskUserQuestion => {
        const q = item as any
        return {
          index: typeof q?.index === 'number' ? q.index : idx,
          topic: typeof q?.topic === 'string' ? q.topic : undefined,
          question: String(q?.question || ''),
          options: Array.isArray(q?.options) ? q.options.map((o: unknown) => String(o)) : [],
        }
      })
      .filter((q): q is AskUserQuestion => Boolean(q.question))

    const req: PendingAskUserRequest = {
      requestId: message.id,
      toolCallId,
      questions,
      raw: message,
    }
    const next = new Map(prev)
    next.set(sid, {
      ...session,
      pendingAskUserRequests: [...(session.pendingAskUserRequests || []), req],
    })
    return next
  }

  return prev
}

export function applyTurnEnd(
  prev: Map<string, SessionBuffer>,
  sid: string,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev
  const next = new Map(prev)
  next.set(sid, { ...session, isRunning: false, isCancelling: false })
  return next
}

export function appendDebugTrace(
  prev: Map<string, SessionBuffer>,
  sid: string,
  line: string,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev
  const existing = Array.isArray(session.debugTrace) ? session.debugTrace : []
  const ts = new Date().toISOString()
  const nextTrace = [...existing, `[${ts}] ${line}`]
  const maxLines = debugTraceMaxLinesOverride ?? (isTraceChainEnabled() ? 2000 : 200)
  const clipped =
    nextTrace.length > maxLines ? nextTrace.slice(nextTrace.length - maxLines) : nextTrace
  const next = new Map(prev)
  next.set(sid, { ...session, debugTrace: clipped })
  return next
}

export function clearDebugTrace(
  prev: Map<string, SessionBuffer>,
  sid: string,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev
  const next = new Map(prev)
  next.set(sid, { ...session, debugTrace: [] })
  return next
}

function clipSetupOutput(content: string): string {
  if (content.length <= MAX_SETUP_OUTPUT_CHARS) return content
  return content.slice(content.length - MAX_SETUP_OUTPUT_CHARS)
}

let debugTraceMaxLinesOverride: number | null = null

export function setDebugTraceMaxLinesOverride(maxLines: number | null | undefined): void {
  if (maxLines === null || typeof maxLines === 'undefined') {
    debugTraceMaxLinesOverride = null
    return
  }
  if (typeof maxLines !== 'number' || !Number.isFinite(maxLines)) return
  debugTraceMaxLinesOverride = Math.min(10_000, Math.max(1, Math.floor(maxLines)))
}

function buildSetupFailureError(event: Extract<SetupScriptEvent, { type: 'finished' }>): string {
  if (event.error) return event.error
  if (event.signal) return `Setup script terminated (${event.signal})`
  if (typeof event.exitCode === 'number') return `Setup script exited with code ${event.exitCode}`
  return 'Setup script failed'
}

export function applySetupScriptEvent(
  prev: Map<string, SessionBuffer>,
  sid: string,
  event: SetupScriptEvent,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev

  if (event.type === 'started') {
    const next = new Map(prev)
    next.set(sid, {
      ...session,
      isSetupRunning: true,
      setupScript: {
        script: String(event.script || ''),
        status: 'running',
        output: '',
        exitCode: null,
      },
    })
    return next
  }

  if (event.type === 'output') {
    const next = new Map(prev)
    const existing = session.setupScript || {
      script: '',
      status: 'running' as const,
      output: '',
      exitCode: null,
    }
    next.set(sid, {
      ...session,
      isSetupRunning: true,
      setupScript: {
        ...existing,
        status: 'running',
        output: clipSetupOutput(`${existing.output || ''}${String(event.data || '')}`),
      },
    })
    return next
  }

  const status: SetupScriptStatus = event.success ? 'completed' : 'failed'
  const next = new Map(prev)
  const existing = session.setupScript || {
    script: '',
    status: 'running' as const,
    output: '',
    exitCode: null,
  }
  next.set(sid, {
    ...session,
    isSetupRunning: false,
    setupScript: {
      ...existing,
      status,
      exitCode: event.exitCode,
      ...(event.success ? { error: undefined } : { error: buildSetupFailureError(event) }),
    },
  })
  return next
}

export function markSetupScriptSkipped(
  prev: Map<string, SessionBuffer>,
  sid: string,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev
  const existing = session.setupScript || {
    script: '',
    status: 'idle' as const,
    output: '',
    exitCode: null,
  }
  const next = new Map(prev)
  next.set(sid, {
    ...session,
    isSetupRunning: false,
    setupScript: {
      ...existing,
      status: 'skipped',
      error: undefined,
    },
  })
  return next
}

export function applyStdout(
  prev: Map<string, SessionBuffer>,
  sid: string,
  data: string,
): Map<string, SessionBuffer> {
  return updateSessionMessages(prev, sid, (msgs) => {
    const last = msgs[msgs.length - 1]
    if (last && last.role === 'assistant') {
      const lastBlock = last.blocks[last.blocks.length - 1]
      if (lastBlock && lastBlock.kind === 'text') {
        return [
          ...msgs.slice(0, -1),
          {
            ...last,
            blocks: [
              ...last.blocks.slice(0, -1),
              { ...lastBlock, content: lastBlock.content + data },
            ],
          },
        ]
      }
      return [
        ...msgs.slice(0, -1),
        { ...last, blocks: [...last.blocks, { kind: 'text' as const, content: data }] },
      ]
    }
    return [
      ...msgs,
      {
        id: uuidv4(),
        role: 'assistant' as const,
        blocks: [{ kind: 'text' as const, content: data }],
        timestamp: Date.now(),
      },
    ]
  })
}

export function applyStderr(
  prev: Map<string, SessionBuffer>,
  sid: string,
  data: string,
): Map<string, SessionBuffer> {
  if (!/error|failed|authentication/i.test(data)) return prev
  return updateSessionMessages(prev, sid, (msgs) => [
    ...msgs,
    {
      id: uuidv4(),
      role: 'error' as const,
      blocks: [{ kind: 'text' as const, content: data.trim() }],
      timestamp: Date.now(),
    },
  ])
}

export function applyExit(
  prev: Map<string, SessionBuffer>,
  sid: string,
): Map<string, SessionBuffer> {
  return applyTurnEnd(prev, sid)
}

export function applyError(
  prev: Map<string, SessionBuffer>,
  sid: string,
  message: string,
): Map<string, SessionBuffer> {
  const session = prev.get(sid)
  if (!session) return prev
  const next = new Map(prev)
  next.set(sid, {
    ...session,
    isRunning: false,
    messages: [
      ...session.messages,
      {
        id: uuidv4(),
        role: 'error' as const,
        blocks: [{ kind: 'text' as const, content: message }],
        timestamp: Date.now(),
      },
    ],
  })
  return next
}
