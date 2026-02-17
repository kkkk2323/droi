import type { DroidExecManager, DroidExecSendOptions } from './droidExecRunner.ts'

type SessionEvent =
  | { type: 'rpc-notification'; sessionId: string; message: any }
  | { type: 'rpc-request'; sessionId: string; message: any }
  | { type: 'session-id-replaced'; oldSessionId: string; newSessionId: string; reason: string }
  | { type: 'turn-end'; sessionId: string; code: number }
  | { type: 'error'; sessionId: string; message: string }

function extractAssistantTextFromCreateMessage(notification: any): string {
  const msg = notification?.message
  if (!msg || msg.role !== 'assistant') return ''
  const content = msg.content
  if (!Array.isArray(content)) return ''
  const parts: string[] = []
  for (const item of content) {
    if (!item || typeof item !== 'object') continue
    if ((item as any).type === 'text' && typeof (item as any).text === 'string') parts.push((item as any).text)
  }
  return parts.join('')
}

function getSessionNotificationPayload(rpcNotification: any): any {
  if (!rpcNotification || rpcNotification.method !== 'droid.session_notification') return null
  return (rpcNotification.params as any)?.notification ?? null
}

export async function runDroidAndCaptureAssistantText(params: {
  execManager: DroidExecManager
  send: DroidExecSendOptions
  timeoutMs?: number
}): Promise<string> {
  const timeoutMs = typeof params.timeoutMs === 'number' ? params.timeoutMs : 30000
  const { execManager } = params
  let currentSessionId = params.send.sessionId

  let done = false
  let lastError = ''
  let sawRpcRequest = false
  let snapshotText = ''
  let deltaText = ''

  return new Promise<string>((resolve, reject) => {
    const timer = setTimeout(() => {
      if (done) return
      done = true
      try { execManager.cancel(currentSessionId) } catch {}
      try { execManager.disposeSession(currentSessionId) } catch {}
      unsub()
      reject(new Error('Timed out generating text'))
    }, timeoutMs)

    const finish = (err?: Error) => {
      if (done) return
      done = true
      clearTimeout(timer)
      try { execManager.disposeSession(currentSessionId) } catch {}
      unsub()
      if (err) reject(err)
      else resolve((snapshotText || deltaText).trim())
    }

    const unsub = execManager.onEvent((ev: any) => {
      const e = ev as SessionEvent
      if (!e) return

      if (e.type === 'session-id-replaced') {
        if (e.oldSessionId === currentSessionId) currentSessionId = e.newSessionId
        return
      }

      if (e.sessionId !== currentSessionId) return

      if (e.type === 'error') {
        lastError = String((e as any).message || '')
        return
      }

      if (e.type === 'rpc-request') {
        sawRpcRequest = true
        try { execManager.cancel(currentSessionId) } catch {}
        return
      }

      if (e.type === 'rpc-notification') {
        const payload = getSessionNotificationPayload((e as any).message)
        if (!payload) return
        if (payload.type === 'assistant_text_delta' && typeof payload.textDelta === 'string') {
          deltaText += payload.textDelta
        } else if (payload.type === 'create_message') {
          const t = extractAssistantTextFromCreateMessage(payload)
          if (t) snapshotText = t
        }
        return
      }

      if (e.type === 'turn-end') {
        const code = typeof (e as any).code === 'number' ? (e as any).code : 1
        if (sawRpcRequest) return finish(new Error('Text generation attempted to use tools; please enter a message manually.'))
        if (code !== 0) return finish(new Error(lastError || 'Failed to generate text'))
        const finalText = (snapshotText || deltaText).trim()
        if (!finalText) return finish(new Error('Generated text was empty'))
        return finish()
      }
    })

    void execManager.send(params.send).catch((err) => {
      const msg = err instanceof Error ? err.message : String(err)
      finish(new Error(msg || 'Failed to start text generation'))
    })
  })
}

export function stripCodeFences(text: string): string {
  const t = String(text || '').trim()
  if (!t) return ''
  const fenceMatch = t.match(/^```[a-zA-Z0-9_-]*\n([\s\S]*?)\n```$/)
  if (fenceMatch) return String(fenceMatch[1] || '').trim()
  return t
}

export function extractFirstJsonObject(text: string): string {
  const t = stripCodeFences(text)
  const start = t.indexOf('{')
  const end = t.lastIndexOf('}')
  if (start === -1 || end === -1 || end <= start) return ''
  return t.slice(start, end + 1)
}
