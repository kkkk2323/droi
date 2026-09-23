// The first message typed on the New session page travels to the Session
// view through here, since the Session does not exist until the Daemon
// answers and the view mounts on its own route.
import type { ImageAttachment } from './attachments'

export interface PendingPrompt {
  text: string
  images: ImageAttachment[]
}

const pending = new Map<string, PendingPrompt>()

export function setPendingPrompt(sessionId: string, prompt: PendingPrompt): void {
  pending.set(sessionId, prompt)
}

/** Returns the queued message once, or null. */
export function takePendingPrompt(sessionId: string): PendingPrompt | null {
  const prompt = pending.get(sessionId) ?? null
  pending.delete(sessionId)
  return prompt
}
