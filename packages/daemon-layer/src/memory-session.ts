// Memory Sessions are the Desktop Shell's own model work on the Daemon (ADR
// 0011). The Shell tags them and every Client leaves them unlisted, as it does
// a Draft Session. No dependencies: the Shell's main process imports it too.

export const MEMORY_SESSION_TAG = 'droi.memory'

export function isMemorySession(tags: ReadonlyArray<{ name: string }> | undefined): boolean {
  return tags?.some((t) => t.name === MEMORY_SESSION_TAG) ?? false
}
