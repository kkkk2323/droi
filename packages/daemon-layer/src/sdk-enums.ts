// The SDK types these enums but does not export their runtime values, so the
// Client carries the string literals. Verified against @factory/droid-sdk 0.9.1.
import type { MultiSessionStateManager } from '@factory/droid-sdk'

export type SessionEventName = Parameters<
  MultiSessionStateManager['subscribeToSessionEvents']
>[0][number]

const event = (name: string): SessionEventName => name as unknown as SessionEventName

export const SESSION_EVENT: Record<
  | 'loadStateChanged'
  | 'messageThreadUpdated'
  | 'workingStateChanged'
  | 'settingsUpdated'
  | 'queuedMessagesUpdated'
  | 'todoListUpdated'
  | 'streamingPlaceholderUpdated'
  | 'metadataUpdated'
  | 'subagentInvocationSummaryUpdated',
  SessionEventName
> = {
  loadStateChanged: event('load_state_changed'),
  messageThreadUpdated: event('message_thread_updated'),
  workingStateChanged: event('working_state_changed'),
  settingsUpdated: event('settings_updated'),
  queuedMessagesUpdated: event('queued_messages_updated'),
  todoListUpdated: event('todo_list_updated'),
  streamingPlaceholderUpdated: event('streaming_placeholder_updated'),
  metadataUpdated: event('metadata_updated'),
  subagentInvocationSummaryUpdated: event('subagent_invocation_summary_updated'),
}

/** A subagent's run as the Daemon reports it (the SDK's TaskInvocationStatus). */
export type SubagentStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export const LOAD_STATE = {
  notLoaded: 'NOT_LOADED',
  loading: 'LOADING',
  loaded: 'LOADED',
} as const

export type LoadState = (typeof LOAD_STATE)[keyof typeof LOAD_STATE]
