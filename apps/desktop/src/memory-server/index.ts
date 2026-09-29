// Memory Server entry (ADR 0010): the stdio MCP Server the Runtime Overlay
// registers as `droi-memory`, run by Electron's own Node (ELECTRON_RUN_AS_NODE).
import { openMemoryStore } from '../memory/store'
import { serveMemory } from '../memory/mcp-server'
import { findSessionCwd, sessionsDir } from '../memory/session-workspace'

const dir = process.env['DROI_MEMORY_DIR']
if (!dir) {
  process.stderr.write('droi-memory: DROI_MEMORY_DIR is not set\n')
  process.exit(1)
}

const sessions = sessionsDir()
const workspaces = new Map<string, string | null>()
const store = openMemoryStore(dir)
await serveMemory(process.stdin, process.stdout, {
  store,
  workspaceOf(sessionId) {
    // A miss is not cached: the Daemon may not have written the file yet.
    const known = workspaces.get(sessionId) ?? findSessionCwd(sessions, sessionId)
    if (known) workspaces.set(sessionId, known)
    return known
  },
})
store.close()
