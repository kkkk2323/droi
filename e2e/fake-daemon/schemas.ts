// Schema validation for the Fake Daemon. Outbound messages are checked with the
// zod schemas the SDK exports; the SDK does not export the Daemon request
// envelopes, so inbound frames get an envelope check here and rely on the SDK
// (which built them) for the params.
import {
  AddUserMessageResultSchema,
  CloseSessionResultSchema,
  DaemonRequestPermissionSchema,
  InitializeSessionResultSchema,
  InterruptSessionResultSchema,
  LoadSessionResultSchema,
  RenameSessionResultSchema,
  SessionNotificationParamsSchema,
  UpdateSessionSettingsResultSchema,
} from '@factory/droid-sdk'
import { z } from 'zod'
import { JsonRpcRequestSchema, type JsonRpcRequest } from './protocol'

interface ZodLike {
  safeParse(value: unknown): { success: boolean; error?: { message: string } }
}

const AuthenticateResult = z.object({ userId: z.string(), orgId: z.string() })
const ConnectionStatusParams = z.object({
  isDroidCLIInPath: z.boolean(),
  droidCLIVersion: z.string().optional(),
  homedir: z.string(),
  platform: z.enum(['darwin', 'linux', 'win32']),
  hostId: z.string().optional(),
})
const AvailableSession = z.object({
  sessionId: z.string(),
  hostId: z.string().optional(),
  updatedAt: z.number(),
  title: z.string().optional(),
  cwd: z.string().optional(),
  repoRoot: z.string().optional(),
  messagesCount: z.number().optional(),
  archivedAt: z.string().optional(),
})
const ListAvailableSessionsResult = z.object({
  sessions: z.array(AvailableSession),
  hasMore: z.boolean(),
  nextCursor: z.number().optional(),
})

/** Result schema per Daemon method; a method missing here is sent unchecked. */
const RESULT_SCHEMAS: Record<string, ZodLike> = {
  'daemon.authenticate': AuthenticateResult,
  'daemon.list_available_sessions': ListAvailableSessionsResult,
  'daemon.load_session': LoadSessionResultSchema,
  'daemon.initialize_session': InitializeSessionResultSchema,
  'daemon.add_user_message': AddUserMessageResultSchema,
  'daemon.interrupt_session': InterruptSessionResultSchema,
  'daemon.rename_session': RenameSessionResultSchema,
  'daemon.update_session_settings': UpdateSessionSettingsResultSchema,
  'daemon.close_session': CloseSessionResultSchema,
}

const NOTIFICATION_SCHEMAS: Record<string, ZodLike> = {
  'daemon.session_notification': SessionNotificationParamsSchema,
  'daemon.connection_status': ConnectionStatusParams,
}

const REQUEST_SCHEMAS: Record<string, ZodLike> = {
  'daemon.request_permission': DaemonRequestPermissionSchema,
}

/** Remembers which method each outbound response answers. */
const methodById = new Map<string | number, string>()

export function rememberMethod(id: string | number, method: string): void {
  methodById.set(id, method)
}

export function validateInboundEnvelope(message: unknown): JsonRpcRequest {
  const parsed = JsonRpcRequestSchema.safeParse(message)
  if (!parsed.success) {
    throw new Error(
      `Fake Daemon received an invalid request envelope: ${parsed.error.message}\n${JSON.stringify(message).slice(0, 500)}`,
    )
  }
  rememberMethod(parsed.data.id, parsed.data.method)
  return parsed.data
}

export function validateOutbound(message: Record<string, unknown>): void {
  const type = message['type']
  if (type === 'response') {
    if ('error' in message) return
    const method = methodById.get(message['id'] as string | number)
    const schema = method ? RESULT_SCHEMAS[method] : undefined
    if (schema) check(schema, message['result'], `${method} result`)
    return
  }
  if (type === 'notification') {
    const method = String(message['method'])
    const schema = NOTIFICATION_SCHEMAS[method]
    if (!schema) throw new Error(`Fake Daemon has no schema for notification ${method}`)
    check(schema, message['params'], `${method} params`)
    return
  }
  if (type === 'request') {
    const method = String(message['method'])
    const schema = REQUEST_SCHEMAS[method]
    if (schema) check(schema, message, method)
    return
  }
  throw new Error(`Fake Daemon tried to send a message without a type: ${JSON.stringify(message)}`)
}

function check(schema: ZodLike, value: unknown, label: string): void {
  const result = schema.safeParse(value)
  if (!result.success) {
    throw new Error(
      `Fake Daemon Scenario produced an invalid ${label}: ${result.error?.message}\n${JSON.stringify(value).slice(0, 800)}`,
    )
  }
}
