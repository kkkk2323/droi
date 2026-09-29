// A Memory Session (ADR 0011): Droi's own model work, done in a hidden Session
// on its own Daemon. The Shell connects through its Gateway like any Client,
// so the Gateway supplies the credential; the Session is tagged so no Client
// lists it, told never to wait on a permission prompt, answers in JSON the
// caller validates, and is archived once its one turn has ended.
import { connectToDaemon, DroidMessageType, type ConnectedDroid } from '@factory/droid-sdk'
import { GATEWAY_API_KEY_PLACEHOLDER, gatewayDaemonUrl } from '@droi/daemon-layer/gateway'
import { MEMORY_SESSION_TAG } from '@droi/daemon-layer/memory-session'

export interface MemorySessionRequest {
  title: string
  /** Where the Session runs; the Daemon needs an existing directory. */
  cwd: string
  modelId: string
  /** The task's instructions, appended to Droid's own system prompt. */
  prompt: string
  /** The one user message: the material to work on. */
  input: string
  /** JSON Schema of the reply. */
  schema: Record<string, unknown>
}

/** Runs one Memory Session and answers its structured reply, still unvalidated. */
export type RunMemorySession = (request: MemorySessionRequest) => Promise<unknown>

export function createMemorySessionRunner(gateway: {
  url: string
  token: string
}): RunMemorySession {
  return async (request) => {
    const droid: ConnectedDroid = await connectToDaemon({
      url: gatewayDaemonUrl(gateway.url, gateway.token),
      auth: { apiKey: GATEWAY_API_KEY_PLACEHOLDER },
    })
    let sessionId: string | null = null
    try {
      const session = await droid.sessions.create({
        cwd: request.cwd,
        title: request.title,
        tags: [{ name: MEMORY_SESSION_TAG }],
        privacyLevel: 'private',
        modelId: request.modelId,
        systemPrompt: { type: 'preset', preset: 'droid', append: request.prompt },
        autoRejectPermissionRequests: true,
        structuredOutputFormat: { type: 'json_schema', schema: request.schema },
      })
      sessionId = session.id
      let reply: unknown = undefined
      let failure: string | null = null
      for await (const message of session.stream(request.input)) {
        if (message.type !== DroidMessageType.Result) continue
        if (message.success) reply = message.structuredOutput
        else {
          failure =
            message.structuredOutputError?.message ??
            message.error?.message ??
            `the turn ended as ${message.subtype}`
        }
      }
      await session.detach()
      if (failure) throw new Error(`Memory Session failed: ${failure}`)
      return reply
    } finally {
      if (sessionId) await droid.sessions.archive(sessionId).catch(() => undefined)
      droid.disconnect()
    }
  }
}
