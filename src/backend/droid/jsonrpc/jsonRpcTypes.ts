export const JSONRPC_VERSION = '2.0' as const
export const FACTORY_API_VERSION = '1.0.0' as const

export interface JsonRpcMeta {
  traceparent?: string
  tracestate?: string
}

export interface JsonRpcBase {
  jsonrpc: typeof JSONRPC_VERSION
  factoryApiVersion: typeof FACTORY_API_VERSION
  _meta?: JsonRpcMeta
}

export interface JsonRpcRequest extends JsonRpcBase {
  type: 'request'
  id: string
  method: string
  params?: unknown
}

export interface JsonRpcError {
  code: number
  message: string
  data?: unknown
}

export interface JsonRpcResponse extends JsonRpcBase {
  type: 'response'
  id: string | null
  result?: unknown
  error?: JsonRpcError
}

export interface JsonRpcNotification extends JsonRpcBase {
  type: 'notification'
  method: string
  params?: unknown
}

export type JsonRpcMessage = JsonRpcRequest | JsonRpcResponse | JsonRpcNotification

export type DroidAutonomyLevel = 'normal' | 'spec' | 'auto-low' | 'auto-medium' | 'auto-high'

export type DroidPermissionOption =
  | 'proceed_once'
  | 'proceed_always'
  | 'proceed_auto_run'
  | 'proceed_auto_run_low'
  | 'proceed_auto_run_medium'
  | 'proceed_auto_run_high'
  | 'proceed_edit'
  | 'cancel'

export interface DroidSessionNotificationAssistantTextDelta {
  type: 'assistant_text_delta'
  messageId: string
  blockIndex: number
  textDelta: string
}

export interface DroidSessionNotificationThinkingTextDelta {
  type: 'thinking_text_delta'
  messageId: string
  blockIndex: number
  textDelta: string
}

export interface DroidSessionNotificationToolUse {
  type: 'tool_use'
  id: string
  name: string
  input: Record<string, unknown>
  thoughtSignature?: string
}

export interface DroidSessionNotificationToolResult {
  type: 'tool_result'
  toolUseId: string
  content?: unknown
  isError?: boolean
}

export interface DroidSessionNotificationToolProgressUpdate {
  type: 'tool_progress_update'
  toolUseId: string
  toolName: string
  update: unknown
}

export interface DroidSessionNotificationWorkingStateChanged {
  type: 'droid_working_state_changed'
  newState: string
}

export interface DroidSessionNotificationError {
  type: 'error'
  message: string
  errorType?: string
  timestamp?: string
  error?: { name: string; message: string }
}

export interface DroidSessionNotificationPermissionResolved {
  type: 'permission_resolved'
  requestId: string
  toolUseIds: string[]
  selectedOption: DroidPermissionOption
}

export type DroidSessionNotification =
  | DroidSessionNotificationAssistantTextDelta
  | DroidSessionNotificationThinkingTextDelta
  | DroidSessionNotificationToolUse
  | DroidSessionNotificationToolResult
  | DroidSessionNotificationToolProgressUpdate
  | DroidSessionNotificationWorkingStateChanged
  | DroidSessionNotificationError
  | DroidSessionNotificationPermissionResolved
  | ({ type: string } & Record<string, unknown>)

