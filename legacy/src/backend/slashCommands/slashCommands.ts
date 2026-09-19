import { readdir, readFile, stat } from 'fs/promises'
import { homedir } from 'os'
import { extname, join, relative, resolve, sep } from 'path'
import type { SlashCommandDef, SlashResolveResult } from '../../shared/protocol.ts'

type ParsedMarkdown = {
  meta: Record<string, string>
  body: string
}

export type SlashCommandEntry = SlashCommandDef & { body: string }

async function isDirectory(dirPath: string): Promise<boolean> {
  try {
    return (await stat(dirPath)).isDirectory()
  } catch {
    return false
  }
}

async function walkMarkdownFiles(rootDir: string): Promise<string[]> {
  const out: string[] = []
  const stack: string[] = [rootDir]
  while (stack.length > 0) {
    const current = stack.pop()!
    let items: Array<{ name: string; isDirectory: boolean; isFile: boolean }> = []
    try {
      const entries = await readdir(current, { withFileTypes: true })
      items = entries.map((e) => ({
        name: e.name,
        isDirectory: e.isDirectory(),
        isFile: e.isFile(),
      }))
    } catch {
      continue
    }

    for (const item of items) {
      const full = join(current, item.name)
      if (item.isDirectory) {
        stack.push(full)
        continue
      }
      if (item.isFile && extname(item.name).toLowerCase() === '.md') out.push(full)
    }
  }
  return out
}

function normalizeCommandName(rootDir: string, filePath: string): string | null {
  const root = resolve(rootDir)
  const full = resolve(filePath)
  if (!full.startsWith(`${root}${sep}`)) return null

  const rel = relative(root, full)
  if (!rel || rel.startsWith('..') || rel.includes(`..${sep}`)) return null
  if (!rel.toLowerCase().endsWith('.md')) return null

  const noExt = rel.slice(0, -3)
  const normalized = noExt
    .split(sep)
    .join('/')
    .replace(/^\/+|\/+$/g, '')
  if (!normalized) return null
  return normalized
}

function splitFrontmatter(raw: string): ParsedMarkdown {
  const text = raw.replace(/^\uFEFF/, '').replace(/\r\n/g, '\n')
  const lines = text.split('\n')
  if (lines[0] !== '---') return { meta: {}, body: raw }

  let end = -1
  for (let i = 1; i < lines.length; i++) {
    if (lines[i] === '---') {
      end = i
      break
    }
  }
  if (end === -1) return { meta: {}, body: raw }

  const meta: Record<string, string> = {}
  for (const line of lines.slice(1, end)) {
    const m = /^([A-Za-z0-9_-]+)\s*:\s*(.*)\s*$/.exec(line)
    if (!m) continue
    meta[m[1]] = m[2]
  }
  const body = lines.slice(end + 1).join('\n')
  return { meta, body }
}

function toDef(params: {
  scope: 'project' | 'user'
  filePath: string
  name: string
  meta: Record<string, string>
}): SlashCommandDef {
  const description = params.meta['description']
  const argumentHint = params.meta['argument-hint'] ?? params.meta['argumentHint']
  return {
    name: params.name,
    scope: params.scope,
    filePath: params.filePath,
    ...(description ? { description } : {}),
    ...(argumentHint ? { argumentHint } : {}),
  }
}

export function getDefaultUserCommandsDir(): string {
  return join(homedir(), '.factory', 'commands')
}

export function getProjectCommandsDir(projectDir: string): string {
  return join(projectDir, '.factory', 'commands')
}

export async function scanSlashCommands(params: {
  projectDir?: string
  userCommandsDir?: string
}): Promise<Map<string, SlashCommandEntry>> {
  const map = new Map<string, SlashCommandEntry>()
  const userRoot = params.userCommandsDir || getDefaultUserCommandsDir()
  const projectRoot =
    params.projectDir && params.projectDir.trim()
      ? getProjectCommandsDir(params.projectDir.trim())
      : ''

  const addFromRoot = async (rootDir: string, scope: 'project' | 'user') => {
    if (!(await isDirectory(rootDir))) return
    const files = await walkMarkdownFiles(rootDir)
    for (const filePath of files) {
      const name = normalizeCommandName(rootDir, filePath)
      if (!name) continue
      let raw = ''
      try {
        raw = await readFile(filePath, 'utf-8')
      } catch {
        continue
      }
      const parsed = splitFrontmatter(raw)
      const def = toDef({ scope, filePath, name, meta: parsed.meta })
      map.set(name, { ...def, body: String(parsed.body || '').trim() })
    }
  }

  // Precedence: project overrides user
  await addFromRoot(userRoot, 'user')
  if (projectRoot) await addFromRoot(projectRoot, 'project')

  return map
}

function applyVars(template: string, vars: Record<string, string>): string {
  let out = template
  for (const [k, v] of Object.entries(vars)) {
    out = out.split(`$${k}`).join(v)
    out = out.split('${' + k + '}').join(v)
  }
  return out
}

export function resolveSlashCommandText(params: {
  text: string
  commands: Map<string, SlashCommandEntry>
  projectDir: string
}): SlashResolveResult {
  const input = String(params.text ?? '')
  const leadingWs = input.match(/^\s*/)?.[0] ?? ''
  const rest = input.slice(leadingWs.length)
  if (!rest.startsWith('/')) return { matched: false, expandedText: input }

  if (rest.startsWith('//')) {
    // Escape: `//foo` => literal `/foo`
    return { matched: false, expandedText: `${leadingWs}${rest.slice(1)}` }
  }

  const afterSlash = rest.slice(1)
  const m = afterSlash.match(/\s/)
  const splitIdx = m ? (m.index ?? -1) : -1
  const name = (splitIdx >= 0 ? afterSlash.slice(0, splitIdx) : afterSlash).trim()
  if (!name) return { matched: false, expandedText: input }

  const args = splitIdx >= 0 ? afterSlash.slice(splitIdx).trim() : ''
  const entry = params.commands.get(name)
  if (!entry) return { matched: false, expandedText: input }

  try {
    const expandedBody = applyVars(entry.body, {
      ARGUMENTS: args,
      PROJECT_DIR: params.projectDir,
    }).trim()
    const { body: _ignored, ...command } = entry
    return { matched: true, expandedText: expandedBody, command }
  } catch (err) {
    const { body: _ignored, ...command } = entry
    return {
      matched: false,
      expandedText: input,
      command,
      error: err instanceof Error ? err.message : String(err),
    }
  }
}
