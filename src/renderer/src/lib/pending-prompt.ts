// The first message typed on the New session page travels to the Session
// view through here, since the Session does not exist until the Daemon
// answers and the view mounts on its own route.
const pending = new Map<string, string>()

export function setPendingPrompt(sessionId: string, text: string): void {
  pending.set(sessionId, text)
}

/** Returns the queued message once, or null. */
export function takePendingPrompt(sessionId: string): string | null {
  const text = pending.get(sessionId) ?? null
  pending.delete(sessionId)
  return text
}
