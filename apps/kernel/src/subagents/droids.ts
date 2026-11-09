import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { basename, join } from 'node:path'
import { split } from '../resources/index.ts'

/**
 * A droid: a kind of subagent the `task` tool starts, with its own prompt, model and tools, as Factory Droid's
 * custom droids are. Two are built in, `worker` and `explorer`; a person's own live in `<userDir>/droids/<name>.md`
 * (`~/.droi/droids`), and one named as a built-in takes its place.
 *
 * The file is Markdown with YAML frontmatter, its body the droid's prompt:
 *
 *     ---
 *     name: code-reviewer
 *     description: Reviews a diff for correctness risks
 *     model: inherit            # or a model id, or provider/modelId
 *     reasoningEffort: high     # optional: off, minimal, low, medium, high, xhigh, max
 *     tools: read-only          # optional: all (default), read-only, or a list of tool names
 *     ---
 *     You are the team's senior reviewer. ...
 */
export type Droid = {
  name: string
  description: string
  prompt: string
  /** A model id or `provider/modelId`; absent (or `inherit`) is the calling conversation's model. */
  model?: string
  reasoningEffort?: string
  /** `all`, `read-only`, or the tools it may use by name. Absent is `all`. */
  tools?: 'all' | 'read-only' | string[]
  source: 'builtin' | 'personal'
  /** The file of a personal droid. */
  path?: string
}

export type DroidFields = Omit<Droid, 'source' | 'path'>
export type DroidDiagnostic = { path: string; message: string }

export const REASONING_EFFORTS = [
  'off',
  'minimal',
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
] as const

/** A droid's name: what `subagent_type` says and its file is called. */
const NAME = /^[a-z0-9][a-z0-9_-]{0,63}$/

export const BUILTIN_DROIDS: readonly Droid[] = [
  {
    name: 'worker',
    description:
      'General-purpose agent for a self-contained task: researching, changing code, running commands. Use it when the work would fill this conversation with output it will not need again.',
    prompt: [
      'You are a worker subagent. Do the task you are given completely, with the tools you have, and verify what you',
      'did. Then answer with what you did and found: the files you changed, the commands you ran and their results,',
      'and anything left undone and why.',
    ].join(' '),
    source: 'builtin',
  },
  {
    name: 'explorer',
    description:
      'Read-only agent for finding things in the codebase: where something is defined or used, how a flow works. It cannot change files or run commands.',
    prompt: [
      'You are an explorer subagent. Find what you are asked for by reading and searching: start from the paths and',
      'names in the task, search narrowly, and read only what you need. Do not guess. Answer with what you found, each',
      'claim with its file and line, and say what you could not find.',
    ].join(' '),
    tools: 'read-only',
    source: 'builtin',
  },
]

export function droidsDir(userDir: string): string {
  return join(userDir, 'droids')
}

/** The droids there are: the built-ins, with a person's own in place of one of the same name, then the rest. */
export function loadDroids(userDir: string): { droids: Droid[]; diagnostics: DroidDiagnostic[] } {
  const diagnostics: DroidDiagnostic[] = []
  const personal = new Map<string, Droid>()
  const dir = droidsDir(userDir)
  let files: string[] = []
  try {
    files = readdirSync(dir).filter((file) => file.endsWith('.md'))
  } catch {
    // No folder: no droids of a person's own yet.
  }
  for (const file of files.sort()) {
    const path = join(dir, file)
    const read = readDroid(path)
    if ('error' in read) {
      diagnostics.push({ path, message: read.error })
      continue
    }
    if (personal.has(read.name)) {
      diagnostics.push({ path, message: `another file is droid ${read.name} too` })
      continue
    }
    personal.set(read.name, read)
  }
  const droids = BUILTIN_DROIDS.map((droid) => personal.get(droid.name) ?? droid)
  for (const droid of personal.values()) {
    if (!BUILTIN_DROIDS.some((builtin) => builtin.name === droid.name)) droids.push(droid)
  }
  return { droids, diagnostics }
}

function readDroid(path: string): Droid | { error: string } {
  let parsed: ReturnType<typeof split>
  try {
    parsed = split(readFileSync(path, 'utf8'))
  } catch (cause) {
    return { error: cause instanceof Error ? cause.message : 'invalid YAML frontmatter' }
  }
  const { frontmatter, body } = parsed
  const name = typeof frontmatter['name'] === 'string' ? frontmatter['name'] : basename(path, '.md')
  const fields = droidFields({ ...frontmatter, name, prompt: body })
  if ('error' in fields) return fields
  return { ...fields, source: 'personal', path }
}

/** A droid's fields as a person or a file gives them, checked; an error says what is wrong. */
export function droidFields(raw: Record<string, unknown>): DroidFields | { error: string } {
  const name = raw['name']
  if (typeof name !== 'string' || !NAME.test(name)) {
    return { error: 'a name of lower-case letters, digits, - and _ is required' }
  }
  const description = raw['description']
  if (typeof description !== 'string' || description.trim() === '') {
    return { error: 'a description is required' }
  }
  const prompt = raw['prompt']
  if (typeof prompt !== 'string' || prompt.trim() === '') {
    return { error: 'a prompt is required' }
  }
  const droid: DroidFields = { name, description: description.trim(), prompt: prompt.trim() }
  const model = raw['model']
  if (model !== undefined && model !== null && model !== 'inherit' && model !== '') {
    if (typeof model !== 'string') return { error: 'model must be a model id or inherit' }
    droid.model = model.trim()
  }
  const effort = raw['reasoningEffort']
  if (effort !== undefined && effort !== null && effort !== '') {
    if (typeof effort !== 'string' || !(REASONING_EFFORTS as readonly string[]).includes(effort)) {
      return { error: `reasoningEffort must be one of ${REASONING_EFFORTS.join(', ')}` }
    }
    droid.reasoningEffort = effort
  }
  const tools = raw['tools']
  if (tools !== undefined && tools !== null && tools !== 'all') {
    if (tools === 'read-only') droid.tools = 'read-only'
    else if (Array.isArray(tools) && tools.every((tool) => typeof tool === 'string')) {
      droid.tools = tools as string[]
    } else return { error: 'tools must be all, read-only or a list of tool names' }
  }
  return droid
}

/** Write a person's droid to its file, in place of the one of that name; the file's path. */
export function writeDroid(userDir: string, droid: DroidFields): string {
  const dir = droidsDir(userDir)
  mkdirSync(dir, { recursive: true })
  const lines = ['---', `name: ${droid.name}`, `description: ${JSON.stringify(droid.description)}`]
  if (droid.model !== undefined) lines.push(`model: ${JSON.stringify(droid.model)}`)
  if (droid.reasoningEffort !== undefined) lines.push(`reasoningEffort: ${droid.reasoningEffort}`)
  if (droid.tools !== undefined) {
    lines.push(
      `tools: ${typeof droid.tools === 'string' ? droid.tools : JSON.stringify(droid.tools)}`,
    )
  }
  lines.push('---', '', droid.prompt, '')
  // A droid renamed from another file keeps one file: the old one goes.
  const existing = loadDroids(userDir).droids.find(
    (each) => each.name === droid.name && each.source === 'personal',
  )
  const path = join(dir, `${droid.name}.md`)
  if (existing?.path !== undefined && existing.path !== path) rmSync(existing.path, { force: true })
  writeFileSync(path, lines.join('\n'))
  return path
}

/** Remove a person's droid; false when there was none. A built-in one stays. */
export function deleteDroid(userDir: string, name: string): boolean {
  const found = loadDroids(userDir).droids.find(
    (droid) => droid.name === name && droid.source === 'personal',
  )
  if (found?.path === undefined || !existsSync(found.path)) return false
  rmSync(found.path)
  return true
}
