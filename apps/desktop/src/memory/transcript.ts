// What Memory reads from a Session's transcript (the Daemon's JSONL session
// file, `transcript_path` in hook input): the user's own prompts and the
// assistant's prose. Tool calls, tool results and injected reminders stay out;
// they are noise for extraction and would inflate the turn count.
import { readFileSync } from 'node:fs'

export interface TranscriptTurn {
  role: 'user' | 'assistant'
  text: string
}

const INJECTED = /^\s*<(system-reminder|memory-context)[\s>]/

interface Block {
  type?: unknown
  text?: unknown
}

export function parseTranscript(jsonl: string): TranscriptTurn[] {
  const turns: TranscriptTurn[] = []
  for (const line of jsonl.split('\n')) {
    if (!line.trim()) continue
    let entry: { type?: unknown; message?: { role?: unknown; content?: unknown } }
    try {
      entry = JSON.parse(line) as typeof entry
    } catch {
      continue
    }
    if (entry.type !== 'message' || !entry.message) continue
    const { role, content } = entry.message
    if (role !== 'user' && role !== 'assistant') continue
    const blocks: Block[] =
      typeof content === 'string'
        ? [{ type: 'text', text: content }]
        : Array.isArray(content)
          ? content
          : []
    if (blocks.some((b) => b.type === 'tool_result')) continue
    const text = blocks
      .flatMap((b) =>
        b.type === 'text' && typeof b.text === 'string' && !INJECTED.test(b.text)
          ? [b.text.trim()]
          : [],
      )
      .filter(Boolean)
      .join('\n\n')
    if (text) turns.push({ role, text })
  }
  return turns
}

export function readTranscript(path: string): TranscriptTurn[] {
  try {
    return parseTranscript(readFileSync(path, 'utf8'))
  } catch {
    return []
  }
}

export function userPromptCount(turns: readonly TranscriptTurn[]): number {
  return turns.filter((t) => t.role === 'user').length
}
