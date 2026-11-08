import { readdirSync, readFileSync, realpathSync, statSync } from 'node:fs'
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path'

/**
 * What the kernel loads from files around a conversation: skills, prompt templates and the `AGENTS.md`
 * chain.
 *
 * All three share the same two roots — the user's own directory, which the Host gives the kernel, and the
 * conversation's working directory — and the same promise: **the files are the source of truth, read fresh
 * every time**. Nothing here is imported from Pi's coding agent: that package is a CLI with its own settings
 * file and its own idea of an agent directory, and copying a thousand lines of its resource loader would
 * bring the settings file along with it. The rules are, though, its rules, so a person who already keeps
 * skills in `~/.factory/skills` recognises what happens.
 */

/** One skill, as `skills.list` and `resources.list` report it. */
export type SkillInfo = {
  name: string
  /** `personal` for the user's own directory, `workspace` for the conversation's. */
  scope: 'personal' | 'workspace'
  /** The `SKILL.md` itself: what the model is told to read. */
  path: string
  description: string
  /** A skill the model may not invoke by itself; it is not offered in the prompt. */
  hidden: boolean
}

/** One slash command the kernel expands from a file. */
export type PromptInfo = {
  name: string
  scope: 'personal' | 'workspace'
  path: string
  description?: string
  /** The body, with `$ARGUMENTS` and `$1..$n` still in it. */
  body: string
}

/** One `AGENTS.md`, and what it says. */
export type ContextFileInfo = { path: string; bytes: number; text: string }

export type LoadedResources = {
  skills: SkillInfo[]
  prompts: PromptInfo[]
  contextFiles: ContextFileInfo[]
  /** Files that look like one of the three but could not be used, and why. Never fatal. */
  diagnostics: { path: string; message: string }[]
}

/** Where a conversation's resources come from. */
export type ResourceRoots = {
  /** The user's own directory (`DROI_USER_DIR`, `~/.droi`). */
  userDir: string
  /** The conversation's `cwd`, or undefined when it has none. */
  workspace: string | undefined
}

/** A skill name is the Agent Skills rule: lower-case, digits and single hyphens, never at the ends. */
const NAME = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
const MAX_NAME = 64
const MAX_DESCRIPTION = 1024

/**
 * Read everything for one conversation.
 *
 * A workspace file wins over a personal one of the same name — the repository is the more specific answer —
 * and the loser is reported as a diagnostic rather than silently dropped.
 */
export function loadResources(roots: ResourceRoots): LoadedResources {
  const diagnostics: LoadedResources['diagnostics'] = []
  const skills = loadSkills(roots, diagnostics)
  const prompts = merge(
    scanPrompts(join(roots.userDir, 'prompts'), 'personal', diagnostics),
    roots.workspace === undefined
      ? []
      : scanPrompts(join(roots.workspace, '.factory', 'prompts'), 'workspace', diagnostics),
    diagnostics,
  )
  return {
    skills,
    prompts,
    contextFiles: loadContextChain(roots, diagnostics),
    diagnostics,
  }
}

/** The skills alone, for a prompt section that has no use for the rest. */
export function loadSkills(
  roots: ResourceRoots,
  diagnostics: LoadedResources['diagnostics'] = [],
): SkillInfo[] {
  return merge(
    scanSkills(join(roots.userDir, 'skills'), 'personal', diagnostics),
    roots.workspace === undefined
      ? []
      : scanSkills(join(roots.workspace, '.factory', 'skills'), 'workspace', diagnostics),
    diagnostics,
  ).map(({ body: _body, ...rest }) => rest)
}

/** The `AGENTS.md` chain alone, in the order it applies. */
export function loadContextChain(
  roots: ResourceRoots,
  diagnostics: LoadedResources['diagnostics'] = [],
): ContextFileInfo[] {
  return loadContextFiles(roots, diagnostics).map((file) => ({
    path: file.path,
    bytes: Buffer.byteLength(file.text, 'utf8'),
    text: file.text,
  }))
}

/** A skill's instructions: its file without the frontmatter. Undefined when it can no longer be read. */
export function skillBody(path: string): string | undefined {
  try {
    return split(readFileSync(path, 'utf8')).body
  } catch {
    return undefined
  }
}

/** A skill keeps its body out of the list: what a client shows is the name and the sentence. */
function scanSkills(
  root: string,
  scope: SkillInfo['scope'],
  diagnostics: LoadedResources['diagnostics'],
): (SkillInfo & { body: string })[] {
  const found: (SkillInfo & { body: string })[] = []
  for (const file of markdownFiles(root, diagnostics)) {
    const raw = read(file, diagnostics)
    if (raw === undefined) continue
    let parsed: ReturnType<typeof split>
    try {
      parsed = split(raw)
    } catch {
      diagnostics.push({ path: file, message: 'invalid YAML frontmatter' })
      continue
    }
    const { frontmatter, body } = parsed
    // The file's own directory names the skill unless the frontmatter says otherwise: a person who moves a
    // folder should not have to edit it.
    const name = frontmatter['name'] ??
      (basename(file) === 'SKILL.md' ? basename(dirname(file)) : basename(file, '.md'))
    if (typeof name !== 'string' || name.length > MAX_NAME || !NAME.test(name)) {
      diagnostics.push({ path: file, message: `the name ${JSON.stringify(name)} is not a skill name` })
      continue
    }
    const description = frontmatter['description']
    if (typeof description !== 'string' || description.trim() === '') {
      diagnostics.push({ path: file, message: 'a description is required in the frontmatter' })
      continue
    }
    if (description.length > MAX_DESCRIPTION) {
      diagnostics.push({ path: file, message: `the description is over ${MAX_DESCRIPTION} characters` })
      continue
    }
    found.push({
      name,
      scope,
      path: file,
      description: description.trim(),
      hidden: frontmatter['disable-model-invocation'] === true,
      body,
    })
  }
  return found
}

/**
 * Skills are files called `SKILL.md`. A directory that holds one is a skill and is not searched further;
 * anything else is walked, so `skills/<group>/<name>/SKILL.md` works as well as `skills/<name>/SKILL.md`.
 * A bare `<name>.md` directly under the root counts too, which is how a person writes one by hand.
 */
function markdownFiles(
  root: string,
  diagnostics: LoadedResources['diagnostics'],
): string[] {
  const files: string[] = []
  const visited = new Set<string>()
  const walk = (dir: string, shallow: boolean): void => {
    let entries: string[]
    try {
      const canonical = realpathSync(dir)
      if (visited.has(canonical)) return
      visited.add(canonical)
      entries = readdirSync(dir)
    } catch {
      // A directory that does not exist is the normal case: most people have no skills at all.
      return
    }
    entries.sort()
    const skill = entries.find((entry) => entry === 'SKILL.md')
    if (skill !== undefined) {
      files.push(join(dir, skill))
      return
    }
    for (const entry of entries) {
      const path = join(dir, entry)
      let stats: ReturnType<typeof statSync>
      try {
        stats = statSync(path)
      } catch {
        continue
      }
      if (stats.isDirectory()) walk(path, false)
      else if (shallow && entry.endsWith('.md')) files.push(path)
    }
  }
  walk(root, true)
  return files
}

/** Prompt templates: `<root>/<name>.md`, one file each, with an optional description. */
function scanPrompts(
  root: string,
  scope: PromptInfo['scope'],
  diagnostics: LoadedResources['diagnostics'],
): PromptInfo[] {
  let entries: string[]
  try {
    entries = readdirSync(root).sort()
  } catch {
    return []
  }
  const found: PromptInfo[] = []
  for (const entry of entries) {
    if (!entry.endsWith('.md')) continue
    const path = join(root, entry)
    const raw = read(path, diagnostics)
    if (raw === undefined) continue
    let parsed: ReturnType<typeof split>
    try {
      parsed = split(raw)
    } catch {
      diagnostics.push({ path, message: 'invalid YAML frontmatter' })
      continue
    }
    const { frontmatter, body } = parsed
    const description = frontmatter['description']
    found.push({
      name: entry.slice(0, -'.md'.length),
      scope,
      path,
      ...(typeof description === 'string' && description.trim() !== ''
        ? { description: description.trim() }
        : {}),
      body,
    })
  }
  return found
}

/**
 * The `AGENTS.md` chain, in the order it applies: the user's own first, then from the outermost directory
 * that has one down to the working directory. That is the order Pi's coding agent uses, and the reason is
 * sound: a more specific file can override what a broader one said.
 */
function loadContextFiles(
  roots: ResourceRoots,
  diagnostics: LoadedResources['diagnostics'],
): { path: string; text: string }[] {
  const files: { path: string; text: string }[] = []
  const seen = new Set<string>()
  const shadowed = roots.workspace === undefined ? undefined : shadowedContext(roots.workspace)
  const add = (dir: string): void => {
    const found = contextFileIn(dir)
    if (found === undefined || seen.has(found.path)) return
    if (shadowed !== undefined &&
      join(realpathSync(dir), basename(found.path)) === shadowed) return
    seen.add(found.path)
    const text = read(found.path, diagnostics)
    if (text !== undefined) files.push({ path: found.path, text })
  }
  add(roots.userDir)
  if (roots.workspace !== undefined) {
    const chain: string[] = []
    let current = resolve(roots.workspace)
    for (;;) {
      chain.push(current)
      const parent = dirname(current)
      if (parent === current) break
      current = parent
    }
    for (const dir of chain.reverse()) add(dir)
  }
  return files
}

/** A nested linked worktree's copy replaces the main checkout's file at that same scope. */
function shadowedContext(workspace: string): string | undefined {
  for (let dir = resolve(workspace); ; dir = dirname(dir)) {
    try {
      const dotgit = join(dir, '.git')
      if (statSync(dotgit).isDirectory()) return undefined
      const match = /^gitdir:\s*(.+)\s*$/m.exec(readFileSync(dotgit, 'utf8'))
      if (match !== null) {
        const gitdir = resolve(dir, match[1]!.trim())
        const common = realpathSync(resolve(gitdir, readFileSync(join(gitdir, 'commondir'), 'utf8').trim()))
        const main = dirname(common)
        const root = realpathSync(dir)
        const rel = relative(main, root)
        if (rel === '' || rel === '..' || rel.startsWith(`..${sep}`) || isAbsolute(rel)) return undefined
        if (realpathSync(join(main, '.git')) !== common) return undefined
        const own = contextFileIn(dir)
        return own === undefined ? undefined : join(main, basename(own.path))
      }
    } catch {
      // No git metadata here, or an ordinary directory: normal ancestor inheritance.
    }
    if (dirname(dir) === dir) return undefined
  }
}

/** The names a directory may give its agent instructions, in the order of preference. A `CLAUDE.md` counts. */
const CONTEXT_NAMES = ['AGENTS.override.md', 'AGENTS.md', 'AGENTS.MD', 'CLAUDE.md', 'CLAUDE.MD']

function contextFileIn(dir: string): { path: string } | undefined {
  for (const name of CONTEXT_NAMES) {
    const path = join(dir, name)
    try {
      if (statSync(path).isFile()) return { path }
    } catch {
      // Not there: try the next name.
    }
  }
  return undefined
}

/** Later entries win a name collision; each loser is reported once, with where it lost to. */
function merge<T extends { name: string; path: string }>(
  personal: T[],
  workspace: T[],
  diagnostics: LoadedResources['diagnostics'],
): T[] {
  const byName = new Map<string, T>()
  for (const item of [...personal, ...workspace]) {
    const existing = byName.get(item.name)
    if (existing !== undefined) {
      diagnostics.push({
        path: existing.path,
        message: `another ${item.name} is used instead: ${item.path}`,
      })
    }
    byName.set(item.name, item)
  }
  return [...byName.values()]
}

function read(path: string, diagnostics: LoadedResources['diagnostics']): string | undefined {
  try {
    return readFileSync(path, 'utf8').replace(/^\uFEFF/, '')
  } catch (error) {
    diagnostics.push({ path, message: error instanceof Error ? error.message : 'cannot be read' })
    return undefined
  }
}

/**
 * A frontmatter block, if the file starts with one, and the body after it.
 *
 * Use Bun's YAML parser: existing skills commonly use folded descriptions and quoted scalars.
 * A file without frontmatter is all body.
 */
export function split(text: string): {
  frontmatter: Record<string, unknown>
  body: string
} {
  const normalised = text.replace(/^\uFEFF/, '')
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(normalised)
  if (match === null) return { frontmatter: {}, body: normalised }
  const value: unknown = Bun.YAML.parse(match[1] ?? '')
  const frontmatter = typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown> : {}
  return { frontmatter, body: normalised.slice(match[0].length) }
}
