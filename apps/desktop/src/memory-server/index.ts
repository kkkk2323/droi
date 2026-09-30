// Memory Server entry (ADR 0010): the stdio MCP Server the Runtime Overlay
// registers as `droi-memory`, run by Electron's own Node (ELECTRON_RUN_AS_NODE).
import { openMemoryStore } from '../memory/store'
import { exportMarkdown } from '../memory/markdown'
import { serveMemory } from '../memory/mcp-server'
import {
  findSessionTranscript,
  isMemorySessionTranscript,
  isScratchSessionTranscript,
  readSessionWorkspace,
  sessionsDir,
} from '../memory/session-workspace'

const dir = process.env['DROI_MEMORY_DIR']
if (!dir) {
  process.stderr.write('droi-memory: DROI_MEMORY_DIR is not set\n')
  process.exit(1)
}

const sessions = sessionsDir()
const transcripts = new Map<string, string>()
// A miss is not cached: the Daemon may not have written the file yet.
const transcriptOf = (sessionId: string): string | null => {
  const known = transcripts.get(sessionId) ?? findSessionTranscript(sessions, sessionId)
  if (known) transcripts.set(sessionId, known)
  return known
}
const store = openMemoryStore(dir)
await serveMemory(process.stdin, process.stdout, {
  store,
  workspaceOf(sessionId) {
    const transcript = transcriptOf(sessionId)
    return transcript ? readSessionWorkspace(transcript) : null
  },
  isMemorySession(sessionId) {
    const transcript = transcriptOf(sessionId)
    return transcript ? isMemorySessionTranscript(transcript) : false
  },
  isScratchSession(sessionId) {
    const transcript = transcriptOf(sessionId)
    return transcript ? isScratchSessionTranscript(transcript) : false
  },
  onWrite(slot) {
    try {
      exportMarkdown(store, slot)
    } catch (error) {
      // The database has the write; a stale export is only cosmetic.
      process.stderr.write(`droi-memory: export failed: ${String(error)}\n`)
    }
  },
})
store.close()
