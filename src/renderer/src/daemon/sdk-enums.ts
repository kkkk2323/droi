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
  | 'streamingPlaceholderUpdated'
  | 'metadataUpdated',
  SessionEventName
> = {
  loadStateChanged: event('load_state_changed'),
  messageThreadUpdated: event('message_thread_updated'),
  workingStateChanged: event('working_state_changed'),
  settingsUpdated: event('settings_updated'),
  queuedMessagesUpdated: event('queued_messages_updated'),
  streamingPlaceholderUpdated: event('streaming_placeholder_updated'),
  metadataUpdated: event('metadata_updated'),
}

export const LOAD_STATE = {
  notLoaded: 'NOT_LOADED',
  loading: 'LOADING',
  loaded: 'LOADED',
} as const

export type LoadState = (typeof LOAD_STATE)[keyof typeof LOAD_STATE]
