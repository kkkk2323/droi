import { realpathSync } from 'node:fs'
import { basename, dirname, isAbsolute, join, resolve, sep, win32 } from 'node:path'
import { destructive } from './commands.ts'

/** How much a conversation may do without asking: `ask`, `edits`, `safe` or `all` (1.x's off/low/medium/high). */
export type Autonomy = 'ask' | 'edits' | 'safe' | 'all'
export const AUTONOMIES: readonly Autonomy[] = ['ask', 'edits', 'safe', 'all']

/** A standing answer: a whole tool, or a file (or a directory and what is under it). */
export type Rule = { kind: 'tool'; tool: string } | { kind: 'file'; path: string }

export type Policy = { autonomy: Autonomy; rules: Rule[] }

/**
 * What a tool can do, which decides what each level lets it do without asking. A tool this table does not
 * know is a command: it may do anything.
 */
export type ToolClass = 'read' | 'edit' | 'exec' | 'meta'

const CLASSES: Readonly<Record<string, ToolClass>> = {
  read: 'read',
  ls: 'read',
  grep: 'read',
  find: 'read',
  edit: 'edit',
  write: 'edit',
  bash: 'exec',
  powershell: 'exec',
  bash_background: 'exec',
  codemode: 'exec',
  ask_user: 'meta',
  submit_plan: 'meta',
}

/** The tools whose `command` argument is a command line. */
const COMMAND_TOOLS = new Set(['bash', 'powershell', 'bash_background'])

/** The tools whose `path` argument is the one file they touch. */
const FILE_TOOLS = new Set(['read', 'edit', 'write'])

/**
 * Classes of tools that arrive at run time, by name and by who registered them: an MCP tool its server marks
 * read-only. Two repositories' servers can register one name; the policy sees only the name, so the name is a
 * read only while every registration of it is one.
 */
const added = new Map<string, Map<string, ToolClass>>()

/** Record what `owner`'s tool of this name can do; `undefined` withdraws `owner`'s registration. */
export function setToolClass(name: string, owner: string, kind: ToolClass | undefined): void {
  const owners = added.get(name) ?? new Map<string, ToolClass>()
  if (kind === undefined) owners.delete(owner)
  else owners.set(owner, kind)
  if (owners.size === 0) added.delete(name)
  else added.set(name, owners)
}

export function toolClass(name: string): ToolClass {
  const builtIn = CLASSES[name]
  if (builtIn !== undefined) return builtIn
  const kinds = new Set(added.get(name)?.values())
  return kinds.size === 1 ? [...kinds][0]! : 'exec'
}

export type Call = { name: string; arguments?: Record<string, unknown> }

export type Decision = { verdict: 'allow' } | { verdict: 'ask'; scopeHint: Autonomy; file?: string }

export type Platform = 'darwin' | 'linux' | 'win32'

/**
 * Whether a call runs at once or waits for a person. Rules come first: a tool or a file a person said to
 * always allow is allowed at every level. Then the level, by what the tool can do.
 */
export function decide(
  call: Call,
  policy: Policy,
  cwd: string | undefined,
  platform: Platform = process.platform as Platform,
): Decision {
  const kind = toolClass(call.name)
  if (kind === 'meta') return { verdict: 'allow' }
  const file = fileOf(call, cwd, platform)
  for (const rule of policy.rules) {
    if (rule.kind === 'tool' && rule.tool === call.name) return { verdict: 'allow' }
    if (rule.kind === 'file' && file !== undefined && covers(rule.path, file, platform)) {
      return { verdict: 'allow' }
    }
  }
  const ask: Decision = {
    verdict: 'ask',
    scopeHint: policy.autonomy,
    ...(file === undefined ? {} : { file }),
  }
  switch (policy.autonomy) {
    case 'all':
      return { verdict: 'allow' }
    case 'ask':
      return ask
    case 'edits':
      return kind === 'exec' ? ask : { verdict: 'allow' }
    case 'safe': {
      if (kind !== 'exec') return { verdict: 'allow' }
      // A script's own calls each come through here, and are judged as they are made.
      if (call.name === 'codemode') return { verdict: 'allow' }
      if (!COMMAND_TOOLS.has(call.name)) return ask
      const command = call.arguments?.['command']
      return typeof command === 'string' && !destructive(command) ? { verdict: 'allow' } : ask
    }
  }
}

/**
 * The file a call touches, as an absolute path: resolved against the conversation's directory, through
 * symlinks where they exist (a file not written yet resolves its directory), lower-cased on Windows.
 */
export function fileOf(
  call: Call,
  cwd: string | undefined,
  platform: Platform,
): string | undefined {
  if (!FILE_TOOLS.has(call.name)) return undefined
  const given = call.arguments?.['path']
  if (typeof given !== 'string' || given === '') return undefined
  return canonical(given, cwd, platform)
}

export function canonical(path: string, cwd: string | undefined, platform: Platform): string {
  const paths = platform === 'win32' ? win32 : { isAbsolute, resolve, dirname, basename, join }
  const absolute = paths.isAbsolute(path) ? path : paths.resolve(cwd ?? '/', path)
  let real = paths.resolve(absolute)
  try {
    real = realpathSync(real)
  } catch {
    try {
      real = paths.join(realpathSync(paths.dirname(real)), paths.basename(real))
    } catch {
      // Neither exists: the path as given, made absolute.
    }
  }
  return platform === 'win32' ? real.toLowerCase() : real
}

/** A file rule covers that file, and everything under it when it names a directory. */
function covers(rule: string, file: string, platform: Platform): boolean {
  const separator = platform === 'win32' ? '\\' : sep
  const root = platform === 'win32' ? rule.toLowerCase() : rule
  return file === root || file.startsWith(root.endsWith(separator) ? root : root + separator)
}
