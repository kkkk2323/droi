import { createHash } from 'node:crypto'
import {
  closeSync,
  lstatSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  writeSync,
} from 'node:fs'
import { join, posix } from 'node:path'

/**
 * The Memory folder (ADR 0031, ADR 0042), as the Kernel reads it: the Memory Summary, the Session Summaries and
 * the Memory Notes. The Kernel writes only `notes/`; the Host writes the rest. This is the Go Memory Server's
 * `apps/native/internal/memory` (folder.go, mcp_server.go, hook.go, secrets.go) in the Kernel, word for word where
 * the model reads the words.
 */

export const SUMMARY_FILE = 'memory_summary.md'
export const SESSION_SUMMARIES_DIR = 'session_summaries'
export const NOTES_DIR = 'notes'
const STATE_FILE = 'state.json'
/** One Memory Note, in characters. */
export const NOTE_MAX_CHARS = 4_000
/** The Memory Notes the Memory Summary has not taken in, shown beside it, in characters. */
export const PENDING_NOTE_CHARS = 3_000

export type MemoryFile = { path: string; size: number; modTime: number }

/** JavaScript's own trim and `\s` are what the Go side copies (its js.go), so they are the reference here. */
const isRegular = (file: string): boolean => {
  try {
    return lstatSync(file).isFile()
  } catch {
    return false
  }
}

/** Names in byte order, as Go's `os.ReadDir` gives them. */
function sortedNames(dir: string): { name: string; regular: boolean }[] {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return []
    throw error
  }
  return entries
    .map((entry) => ({ name: entry.name, regular: entry.isFile() }))
    .sort((a, b) => Buffer.compare(Buffer.from(a.name), Buffer.from(b.name)))
}

/** The Memory Summary, the Session Summaries and the Memory Notes, in that order, each folder by name. */
export function files(dir: string): MemoryFile[] {
  const out: MemoryFile[] = []
  try {
    const info = lstatSync(join(dir, SUMMARY_FILE))
    if (info.isFile()) out.push({ path: SUMMARY_FILE, size: info.size, modTime: info.mtimeMs })
  } catch {
    // No Memory Summary yet.
  }
  for (const sub of [SESSION_SUMMARIES_DIR, NOTES_DIR]) {
    for (const entry of sortedNames(join(dir, sub))) {
      if (!entry.regular || !entry.name.endsWith('.md')) continue
      try {
        const info = lstatSync(join(dir, sub, entry.name))
        out.push({ path: `${sub}/${entry.name}`, size: info.size, modTime: info.mtimeMs })
      } catch {
        // Gone since it was listed.
      }
    }
  }
  return out
}

/** Go's `path.Clean` of a slash path: no trailing slash, `.` for nothing. */
function clean(path: string): string {
  if (path === '') return '.'
  const normal = posix.normalize(path)
  return normal.length > 1 && normal.endsWith('/') ? normal.slice(0, -1) : normal
}

/**
 * The file a path the model gave names, and its path in the folder; anything outside the Memory Summary, the
 * Session Summaries and the Memory Notes is refused with the sentence the model reads.
 */
export function resolveFile(
  dir: string,
  asked: string,
): { file: string; path: string } | { error: string } {
  const rel = asked.replaceAll('\\', '/').trim()
  const path = clean(rel)
  const refused = {
    error: `Memory has no file ${rel}. Name the Memory Summary (${SUMMARY_FILE}), a Session Summary (${SESSION_SUMMARIES_DIR}/...) or a Memory Note (${NOTES_DIR}/...), as memory_list shows them.`,
  }
  if (rel === '' || path.startsWith('/') || path.startsWith('../') || path === '..') return refused
  const slash = path.lastIndexOf('/')
  const parent = path.slice(0, slash + 1)
  const name = path.slice(slash + 1)
  if (
    path !== SUMMARY_FILE &&
    ((parent !== `${SESSION_SUMMARIES_DIR}/` && parent !== `${NOTES_DIR}/`) ||
      !name.endsWith('.md'))
  ) {
    return refused
  }
  const file = join(dir, ...path.split('/'))
  if (!isRegular(file)) return refused
  return { file, path }
}

/** The `key: value` lines that open a Session Summary or a Memory Note, up to the first blank line. */
export function header(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const raw of text.split('\n')) {
    const line = raw.replace(/\r+$/, '')
    if (line.trim() === '') break
    const at = line.indexOf(':')
    if (at < 0) break
    const key = line.slice(0, at)
    if (/[ \t]/.test(key)) break
    out[key] = line.slice(at + 1).trim()
  }
  return out
}

/** A string's words joined by single spaces. */
export function oneLine(text: string): string {
  return text
    .split(/\s+/)
    .filter((word) => word !== '')
    .join(' ')
}

/** The text cut to `n` characters, with an ellipsis where it was cut. */
export function clip(text: string, n: number): string {
  const chars = Array.from(text.trim())
  return chars.length <= n ? chars.join('') : chars.slice(0, n).join('') + '…'
}

const SECRETS: readonly { kind: string; pattern: RegExp }[] = [
  { kind: 'a private key', pattern: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
  { kind: 'an OpenAI or Anthropic API key', pattern: /\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}/ },
  { kind: 'a Factory API key', pattern: /\bfk-[A-Za-z0-9_-]{20,}/ },
  { kind: 'an AWS access key', pattern: /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/ },
  {
    kind: 'a GitHub token',
    pattern: /\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})/,
  },
  { kind: 'a GitLab token', pattern: /\bglpat-[A-Za-z0-9_-]{20,}/ },
  { kind: 'a Slack token', pattern: /\bxox[abposr]-[A-Za-z0-9-]{10,}/ },
  { kind: 'a Google API key', pattern: /\bAIza[0-9A-Za-z_-]{35}\b/ },
  {
    kind: 'a JSON Web Token',
    pattern: /\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/,
  },
  { kind: 'a bearer token', pattern: /\bBearer\s+[A-Za-z0-9._~+/-]{20,}=*/i },
  { kind: 'a password', pattern: /\b(?:password|passwd|pwd)\s*[=:]\s*\S+/i },
  {
    kind: 'an API key or secret',
    pattern:
      /\b(?:api[_-]?key|secret|access[_-]?token|auth[_-]?token)\s*[=:]\s*['"]?[A-Za-z0-9_\-./+]{16,}/i,
  },
]

/** Why the text may not be stored, or undefined when it looks clean. It never repeats the match. */
export function findSecret(text: string): string | undefined {
  for (const secret of SECRETS) {
    if (secret.pattern.test(text)) {
      return `The text looks like it contains ${secret.kind}. Memory never stores credentials; leave the secret out and describe where it lives instead.`
    }
  }
  return undefined
}

/** A file-name-safe form of the text: its lowercase ASCII words and numbers joined by hyphens, within `max`. */
export function slug(text: string, max: number): string {
  const words = text
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter((word) => word !== '')
  let out = ''
  for (const word of words) {
    const next = out === '' ? word : `${out}-${word}`
    if (next.length > max) {
      if (out === '') out = word.slice(0, max)
      break
    }
    out = next
  }
  return out
}

/** A time as Memory's file names begin: 2026-10-27T09-21-03. */
export function timeStamp(at: Date): string {
  return at.toISOString().slice(0, 19).replaceAll(':', '-')
}

/**
 * Save a Memory Note, never replacing one, and answer its path in the folder; a refusal is the sentence the model
 * reads. The Host folds every note into the Memory Summary.
 */
export function writeNote(
  dir: string,
  given: string,
  conversation: string | undefined,
  now: Date,
): { path: string } | { error: string } {
  const text = given.trim()
  if (text === '') return { error: 'The note is empty.' }
  if (Array.from(text).length > NOTE_MAX_CHARS) {
    return {
      error: `A note is at most ${NOTE_MAX_CHARS} characters; keep to what the user asked to remember, forget or correct.`,
    }
  }
  const secret = findSecret(text)
  if (secret !== undefined) return { error: secret }
  let body = `written_at: ${now.toISOString().replace(/\.\d{3}Z$/, 'Z')}\n`
  if (conversation !== undefined && conversation !== '') body += `session_id: ${conversation}\n`
  body += `\n${text}\n`
  mkdirSync(join(dir, NOTES_DIR), { recursive: true })
  const base = `${timeStamp(now)}-${slug(text, 40) || 'note'}`
  for (let i = 1; ; i++) {
    const name = i === 1 ? `${base}.md` : `${base}-${i}.md`
    let fd: number
    try {
      fd = openSync(join(dir, NOTES_DIR, name), 'wx')
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'EEXIST') continue
      throw error
    }
    try {
      writeSync(fd, body)
    } finally {
      closeSync(fd)
    }
    return { path: `${NOTES_DIR}/${name}` }
  }
}

/** The file's lines from `offset`, at most `limit` of them, each after its number. */
export function numbered(path: string, text: string, offset: number, limit: number): string {
  const lines = text.replace(/\n+$/, '').split('\n')
  if (offset > lines.length) return `${path} has ${lines.length} lines.`
  const end = Math.min(lines.length, offset - 1 + limit)
  let out = `${path}, lines ${offset}-${end} of ${lines.length}:\n`
  for (let i = offset - 1; i < end; i++) out += `${i + 1}: ${lines[i]}\n`
  return out
}

/** The query's words, a quoted phrase kept whole, lowercased and each once. */
export function terms(query: string): string[] {
  const out: string[] = []
  for (const match of query.matchAll(/"([^"]+)"|(\S+)/g)) {
    const term = ((match[1] ?? '') + (match[2] ?? '')).trim().toLowerCase()
    if (term !== '' && !out.includes(term)) out.push(term)
  }
  return out
}

/** The lines that hold every term, newest file first; only when none does, the lines that hold the most. */
export function search(dir: string, query: string, limit: number): string {
  const words = terms(query)
  type Hit = { file: string; line: number; text: string; matched: number; modTime: number }
  let hits: Hit[] = []
  for (const file of files(dir)) {
    let text: string
    try {
      text = readFileSync(join(dir, ...file.path.split('/')), 'utf8')
    } catch {
      continue
    }
    text.split('\n').forEach((line, index) => {
      const lower = line.toLowerCase()
      const matched = words.filter((word) => lower.includes(word)).length
      if (matched > 0) {
        hits.push({
          file: file.path,
          line: index + 1,
          text: line.trim(),
          matched,
          modTime: file.modTime,
        })
      }
    })
  }
  const all = hits.filter((hit) => hit.matched >= words.length)
  if (all.length > 0) hits = all
  // Array.prototype.sort is stable, as Go's SliceStable is.
  hits.sort((a, b) => b.matched - a.matched || b.modTime - a.modTime)
  if (hits.length === 0) return `Nothing in Memory matches ${JSON.stringify(query)}.`
  const shown = hits.slice(0, limit)
  let out = `${hits.length} lines in Memory hold ${all.length === 0 ? 'some words of' : 'every word of'} ${JSON.stringify(query)}`
  if (shown.length < hits.length) out += `, the first ${shown.length}`
  out += ' (read a file with memory_read):\n'
  for (const hit of shown) out += `${hit.file}:${hit.line}: ${clip(hit.text, 300)}\n`
  return out
}

/** Memory's files: the Session Summaries newest first, with their day, Workspace and title, then the Notes. */
export function list(dir: string, workspace: string, limit: number): string {
  const summaries: string[] = []
  const notes: string[] = []
  let hasSummary = false
  for (const file of files(dir).reverse()) {
    if (file.path === SUMMARY_FILE) {
      hasSummary = true
    } else if (file.path.startsWith(`${NOTES_DIR}/`)) {
      notes.push(`- ${file.path}`)
    } else {
      let text: string
      try {
        text = readFileSync(join(dir, ...file.path.split('/')), 'utf8')
      } catch {
        continue
      }
      const h = header(text)
      if (
        workspace !== '' &&
        !(h['workspace'] ?? '').toLowerCase().includes(workspace.toLowerCase())
      ) {
        continue
      }
      const day = (h['updated_at'] ?? '').split('T')[0]
      summaries.push(`- ${file.path} · ${day} · ${h['workspace'] ?? ''} · ${h['title'] ?? ''}`)
    }
  }
  let out = ''
  if (hasSummary)
    out += `${SUMMARY_FILE}: the Memory Summary, shown at the start of every Session.\n`
  if (summaries.length === 0) {
    out += 'No Session Summaries'
    if (workspace !== '') out += ` for ${workspace}`
    out += '.\n'
  } else {
    out += `${summaries.length} Session Summaries, newest first`
    let shown = summaries
    if (summaries.length > limit) {
      out += `, the first ${limit}`
      shown = summaries.slice(0, limit)
    }
    out += `:\n${shown.join('\n')}\n`
  }
  if (notes.length > 0) out += `${notes.length} Memory Notes:\n${notes.join('\n')}\n`
  return out
}

// The read policy follows Codex's read path (codex-rs/ext/memories, read_path_v2.md, Apache-2.0), with Droi's tools.
const POLICY = [
  '## Memory',
  '',
  "Droi Memory is on. The Memory Summary below was written from earlier Sessions on this computer. Use it as historical context: apply the user's actual preferences, corrections, decisions and supported task scope.",
  `- Its pointers name Session Summaries (${SESSION_SUMMARIES_DIR}/...) and Memory Notes (${NOTES_DIR}/...). Read a Memory Note it points to before work in that project. Read a Session Summary with memory_read when its evidence, wording, chronology or uncertainty could change your answer; otherwise do not look up history speculatively. Use memory_search when a route you need is missing.`,
  '- Memory is not proof of current behavior. For a claim that may have changed or that matters, check the actual source, and say so when you are unsure. When Memory disagrees with the repository or with the user, they win.',
  '- Droi keeps Memory from finished Sessions on its own; do not try to save what you learn. Only when the user explicitly asks you to remember, forget or correct something, call memory_note with what they asked.',
].join('\n')

const NO_SUMMARY =
  'There is no Memory Summary yet: Droi writes it from Sessions once they have been idle for a while.'

/** The SHA-256 of every input the Memory Summary has taken in, by path, from the Host's `state.json`. */
function takenIn(dir: string): Record<string, string> {
  try {
    const state = JSON.parse(readFileSync(join(dir, STATE_FILE), 'utf8')) as {
      phase2?: { inputs?: Record<string, string> }
    }
    return state.phase2?.inputs ?? {}
  } catch {
    return {}
  }
}

/** The Memory Notes the Memory Summary has not taken in, newest first, within PENDING_NOTE_CHARS. */
function pendingNotes(dir: string): string[] {
  const taken = takenIn(dir)
  const out: string[] = []
  let chars = 0
  for (const entry of sortedNames(join(dir, NOTES_DIR)).reverse()) {
    if (!entry.name.endsWith('.md')) continue
    let bytes: Buffer
    try {
      bytes = readFileSync(join(dir, NOTES_DIR, entry.name))
    } catch {
      continue
    }
    if (taken[`${NOTES_DIR}/${entry.name}`] === createHash('sha256').update(bytes).digest('hex')) {
      continue
    }
    let text = bytes.toString('utf8')
    const blank = text.indexOf('\n\n')
    if (blank >= 0 && Object.keys(header(text)).length > 0) text = text.slice(blank + 2)
    const line = `- ${oneLine(text)}`
    const n = Array.from(line).length
    if (chars + n > PENDING_NOTE_CHARS) break
    chars += n
    out.push(line)
  }
  return out
}

/** What a conversation starts with: the read policy, the Memory Summary and the Notes it has not taken in. */
export function memoryContext(dir: string): string {
  let out = `<memory-context>\n${POLICY}\n\n`
  let summary = ''
  try {
    summary = readFileSync(join(dir, SUMMARY_FILE), 'utf8').trim()
  } catch {
    // No Memory Summary yet.
  }
  out +=
    summary === ''
      ? `${NO_SUMMARY}\n`
      : `========= MEMORY_SUMMARY BEGINS =========\n${summary}\n========= MEMORY_SUMMARY ENDS =========\n`
  const notes = pendingNotes(dir)
  if (notes.length > 0) {
    out += `\nMemory Notes the user asked for that the summary does not include yet, newest first; they win over it:\n${notes.join('\n')}\n`
  }
  return `${out}</memory-context>\n`
}
