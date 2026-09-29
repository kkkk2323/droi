// Memory hook entry (ADR 0010): the Runtime Overlay runs it for SessionStart,
// UserPromptSubmit, PreCompact and SessionEnd with the event as JSON on stdin.
// A hook must never break a Session, so every failure exits 0 silently.
import { openMemoryStore } from '../memory/store'
import { runHook, type HookInput } from '../memory/hook'

async function readStdin(): Promise<string> {
  let text = ''
  for await (const chunk of process.stdin) text += String(chunk)
  return text
}

try {
  const dir = process.env['DROI_MEMORY_DIR']
  if (dir) {
    const input = JSON.parse(await readStdin()) as HookInput
    const store = openMemoryStore(dir)
    try {
      process.stdout.write(runHook(store, input))
    } finally {
      store.close()
    }
  }
} catch (error) {
  process.stderr.write(
    `droi-memory hook: ${error instanceof Error ? error.message : String(error)}\n`,
  )
}
