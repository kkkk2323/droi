// The Kernel's protocol as the Phone App reads it (apps/kernel/protocol/README.md): the
// shapes of the methods and notifications it uses. Ids are numbers on the wire; the rest
// of the layer names a Session by its id as a decimal string.

export type ConversationId = number
export type EntryId = number

/** A Pi entry: one message (or one of Droi's own records) of a transcript. */
export interface Entry {
  id: EntryId
  conversationId: ConversationId
  kind: string
  model?: PiMessage[]
  data?: Record<string, unknown>
  head?: EntryId | 'self'
  byTaskId?: number
}

export interface PiMessage {
  role: 'user' | 'assistant' | 'toolResult' | 'system' | string
  content?: string | PiContent[]
  timestamp?: number
  stopReason?: string
  errorMessage?: string
  toolCallId?: string
  toolName?: string
  isError?: boolean
  details?: unknown
  model?: string
  provider?: string
}

export type PiContent =
  | { type: 'text'; text: string }
  | { type: 'thinking'; thinking: string; durationMs?: number }
  | { type: 'image'; data: string; mimeType: string }
  | { type: 'toolCall'; id: string; name: string; arguments: unknown }
  | { type: string; [key: string]: unknown }

/** `conversation.open`'s view, and what a `conversation.view` push makes of it. */
export interface View {
  conversation: { id: ConversationId; [key: string]: unknown }
  entries: Entry[]
  /** The entry just before the window; absent when the window starts the transcript. */
  before?: EntryId
  docs: Record<string, unknown>
}

export interface ViewPush {
  id: ConversationId
  kind: 'value' | 'full' | 'failed'
  value: View | null
  ops: unknown[]
  retired?: boolean
  error?: { message?: string }
}

export interface LiveRun {
  taskId: number
  inputs: number[]
  turns: number
}

export interface LiveDoc {
  run?: LiveRun | null
  runFailure?: { taskId: number; reason: string; detail?: string } | null
  generation?: { attempt: number; retry?: { at: number; error: string } }
  tools?: Array<{ callId: string; name: string; status: string; output?: string }>
  nestedTools?: Array<{
    parentCallId: string
    callId: string
    name: string
    arguments: unknown
    status: string
    output?: string
    summary?: { isError?: boolean; error?: string }
  }>
  compactions?: Array<{ taskId: number; trigger: string; blocking: boolean }>
  compactionFailure?: { taskId: number; trigger: string; error: string }
}

export interface InboxItem {
  id: number
  mode: 'steer' | 'followUp' | 'write'
  content?: string | PiContent[]
}

export interface AgentDoc {
  model?: { provider: string; modelId: string }
  thinkingLevel?: string
  cwd?: string
  extensions?: unknown
}

/** One row of `conversation.list`. */
export interface ListItem {
  id: ConversationId
  title: string | null
  titleSource?: 'auto' | 'chosen'
  kind: string | null
  workspace: string | null
  repoRoot?: string
  archived: boolean
  createdAt: number
  lastActivityAt: number
  viewedAt: number | null
  preview: string | null
  parentId?: ConversationId
  ownerId?: ConversationId
  busy?: boolean
}

export interface ModelInfo {
  provider: string
  id: string
  name?: string
  contextWindow?: number
  input?: string[]
  thinking?: string[]
}

export type Autonomy = 'ask' | 'edits' | 'safe' | 'all'

export interface Policy {
  autonomy: Autonomy
  rules: unknown[]
}

export interface HarnessInfo {
  version: string
  capabilities: Record<string, boolean>
  models: ModelInfo[]
  limits?: { maxEntriesPerPage?: number; maxConversationsPerPage?: number }
  policy?: Policy
  /** What a new conversation starts with when its create names no model: the computer's Session defaults. */
  agent?: { model: { provider: string; modelId: string }; thinkingLevel?: string }
}

export interface ApprovalOption {
  id: string
  label: string
  scope?: unknown
}

export interface ApprovalRequest {
  requestId: string
  id: ConversationId
  tool: string
  args: Record<string, unknown>
  options: ApprovalOption[]
  scopeHint?: string
}

export interface AskQuestion {
  questionId: string
  question: string
  options?: Array<{ id: string; label: string; description?: string }>
  multiSelect?: boolean
}

export interface AskRequest {
  requestId: string
  id: ConversationId
  questions: AskQuestion[]
  context: string
  blocking?: boolean
}

export type PendingRequest =
  | ({ kind: 'approval' } & ApprovalRequest)
  | ({ kind: 'ask' } & AskRequest)

export interface OpenResult {
  view: View
  policy?: Policy
  pending?: PendingRequest[]
}

/** The Kernel's error codes a Client branches on. */
export const KernelCode = {
  conversationNotFound: -32000,
  conversationBusy: -32001,
  submissionSettled: -32004,
  submissionPlaced: -32005,
  requestNotFound: -32007,
  readOnly: -32008,
  unsupported: -32009,
  conflict: -32013,
} as const

export function sessionIdOf(id: ConversationId): string {
  return String(id)
}

/** The conversation a Session id names; null for an id the Kernel never gave. */
export function conversationIdOf(sessionId: string): ConversationId | null {
  if (!/^\d+$/.test(sessionId)) return null
  const id = Number(sessionId)
  return Number.isSafeInteger(id) ? id : null
}
