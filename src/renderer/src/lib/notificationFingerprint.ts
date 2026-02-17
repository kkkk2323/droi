import type { JsonRpcNotification } from '@/types'

export interface NotificationTraceInfo {
  method: string
  type: string
  fingerprint: string
  traceparentShort: string
  messageId: string
  blockIndex: string
  deltaLen: string
  createMessageId: string
  newState: string
}

let traceChainEnabledOverride: boolean | undefined

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function normalizeForHash(value: unknown): unknown {
  if (Array.isArray(value)) return value.map((item) => normalizeForHash(item))
  if (!isPlainObject(value)) return value
  const out: Record<string, unknown> = {}
  const keys = Object.keys(value).sort()
  for (const key of keys) out[key] = normalizeForHash((value as any)[key])
  return out
}

function fnv1a32(input: string): string {
  let hash = 0x811c9dc5
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193) >>> 0
  }
  return hash.toString(16).padStart(8, '0')
}

function sanitizeToken(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return '-'
  return trimmed.replace(/\s+/g, ' ').replace(/[|]/g, '/')
}

function shortTraceparent(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return '-'
  const normalized = value.trim()
  return normalized.length <= 24 ? normalized : normalized.slice(0, 24)
}

function getSessionNotification(message: JsonRpcNotification): Record<string, unknown> | null {
  if (message.method !== 'droid.session_notification') return null
  const params = message.params
  if (!isPlainObject(params)) return null
  const notification = (params as any).notification
  if (!isPlainObject(notification)) return null
  return notification
}

export function computeNotificationFingerprint(message: JsonRpcNotification): string {
  const normalized = normalizeForHash(message)
  return fnv1a32(JSON.stringify(normalized))
}

export function buildNotificationTraceInfo(message: JsonRpcNotification): NotificationTraceInfo {
  const notification = getSessionNotification(message)
  const method = sanitizeToken(String(message.method || '-'))
  const type = sanitizeToken(typeof notification?.type === 'string' ? notification.type : '-')
  const messageId = sanitizeToken(typeof notification?.messageId === 'string' ? notification.messageId : '-')
  const blockIndex = Number.isFinite(notification?.blockIndex as any) ? String(notification?.blockIndex) : '-'
  const textDelta = typeof notification?.textDelta === 'string' ? notification.textDelta : ''
  const deltaLen = textDelta ? String(textDelta.length) : '-'
  const createMessageId = sanitizeToken(
    typeof (notification as any)?.message?.id === 'string'
      ? String((notification as any).message.id)
      : '-'
  )
  const newState = sanitizeToken(typeof notification?.newState === 'string' ? notification.newState : '-')
  const traceparent = shortTraceparent((message as any)?._meta?.traceparent)
  const fingerprint = computeNotificationFingerprint(message)

  return {
    method,
    type,
    fingerprint,
    traceparentShort: traceparent,
    messageId,
    blockIndex,
    deltaLen,
    createMessageId,
    newState,
  }
}

export function formatNotificationTrace(stage: string, message: JsonRpcNotification): string {
  const info = buildNotificationTraceInfo(message)
  return [
    'trace-chain:',
    `stage=${sanitizeToken(stage)}`,
    `method=${info.method}`,
    `type=${info.type}`,
    `fingerprint=${info.fingerprint}`,
    `traceparent=${info.traceparentShort}`,
    `messageId=${info.messageId}`,
    `blockIndex=${info.blockIndex}`,
    `deltaLen=${info.deltaLen}`,
    `createMessageId=${info.createMessageId}`,
    `newState=${info.newState}`,
  ].join(' ')
}

function isEnabledLike(value: unknown): boolean {
  const normalized = String(value || '').trim().toLowerCase()
  return normalized === '1' || normalized === 'true' || normalized === 'yes' || normalized === 'on'
}

export function setTraceChainEnabledOverride(enabled: boolean | undefined): void {
  traceChainEnabledOverride = typeof enabled === 'boolean' ? enabled : undefined
}

export function isTraceChainEnabled(): boolean {
  if (typeof traceChainEnabledOverride === 'boolean') return traceChainEnabledOverride
  const env = (import.meta as any)?.env
  if (isEnabledLike(env?.VITE_DROID_TRACE_CHAIN)) return true
  if (isEnabledLike(env?.DROID_TRACE_CHAIN)) return true
  if (isEnabledLike((globalThis as any)?.process?.env?.DROID_TRACE_CHAIN)) return true
  if (isEnabledLike((globalThis as any)?.__DROID_TRACE_CHAIN)) return true
  return false
}
