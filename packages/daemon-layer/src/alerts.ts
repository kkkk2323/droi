// When a Session calls for an alert. Each Client decides what an alert does
// (a sound, a notification, a haptic); every Client marks the Session unread.

export type AlertEvent = 'completion' | 'awaiting-input'

/**
 * Follows each Session's working state and says when it calls for an alert,
 * the way Factory App does: once when it starts waiting for an answer, and
 * once when it goes idle after doing something.
 */
export class AlertTracker {
  #active = new Set<string>()
  #awaiting = new Set<string>()

  update(sessionId: string, workingState: string): AlertEvent | null {
    if (workingState === 'idle') {
      this.#awaiting.delete(sessionId)
      return this.#active.delete(sessionId) ? 'completion' : null
    }
    this.#active.add(sessionId)
    if (workingState !== 'waiting_for_tool_confirmation') return null
    if (this.#awaiting.has(sessionId)) return null
    this.#awaiting.add(sessionId)
    return 'awaiting-input'
  }

  /** The Session dropped out of this Client (a reconnect, a close); its next state is no news. */
  forget(sessionId: string): void {
    this.#active.delete(sessionId)
    this.#awaiting.delete(sessionId)
  }
}
