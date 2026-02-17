import { readdir, readFile } from 'fs/promises'
import { homedir } from 'os'
import { join, resolve, sep } from 'path'
import type { SkillDef } from '../../shared/protocol.ts'
import { isDirectory } from '../utils/fs.ts'

function hasPathSegment(p: string, seg: string): boolean {
  const parts = resolve(p).split(sep).filter(Boolean)
  return parts.includes(seg)
}

function extractSkillDescription(markdown: string): string {
  const text = String(markdown || '').replace(/^\uFEFF/, '').replace(/\r\n/g, '\n')
  const lines = text.split('\n')

  // Prefer frontmatter description field
  if (lines[0] === '---') {
    let end = -1
    for (let i = 1; i < lines.length; i++) {
      if (lines[i] === '---') { end = i; break }
    }
    if (end > 0) {
      for (const line of lines.slice(1, end)) {
        const m = /^description\s*:\s*"?(.+?)"?\s*$/.exec(line)
        if (m) return m[1]
      }
    }
  }

  // Fallback: first non-empty line after heading
  let headerIdx = -1
  for (let i = 0; i < lines.length; i++) {
    if (/^#\s+.+/.test(lines[i])) { headerIdx = i; break }
  }
  if (headerIdx < 0) return ''
  for (let i = headerIdx + 1; i < lines.length; i++) {
    const line = lines[i].trim()
    if (!line) continue
    return line
  }
  return ''
}

export function getDefaultUserFactorySkillsDir(): string {
  return join(homedir(), '.factory', 'skills')
}

export function getDefaultAgentsSkillsDir(): string {
  return join(homedir(), '.agents', 'skills')
}

export function getProjectSkillsDir(projectDir: string): string {
  return join(projectDir, '.factory', 'skills')
}

async function scanRoot(rootDir: string, scope: 'project' | 'user'): Promise<SkillDef[]> {
  const out: SkillDef[] = []
  if (!rootDir || !(await isDirectory(rootDir))) return out
  if (hasPathSegment(rootDir, '.system')) return out

  let entries: Array<{ name: string; isDirectory: boolean }> = []
  try {
    const raw = await readdir(rootDir, { withFileTypes: true })
    entries = raw.map((e) => ({ name: e.name, isDirectory: e.isDirectory() }))
  } catch {
    return out
  }

  for (const entry of entries) {
    if (!entry.isDirectory) continue
    const name = entry.name
    if (!name || name.startsWith('.')) continue
    const skillDir = join(rootDir, name)
    if (hasPathSegment(skillDir, '.system')) continue
    const filePath = join(skillDir, 'SKILL.md')
    try {
      const raw = await readFile(filePath, 'utf-8')
      const description = extractSkillDescription(raw)
      out.push({
        name,
        scope,
        filePath,
        ...(description ? { description } : {}),
      })
    } catch {
      // ignore unreadable/missing SKILL.md
    }
  }

  return out
}

export async function scanSkills(params: {
  projectDir?: string
  userFactorySkillsDir?: string
  agentsSkillsDir?: string
}): Promise<SkillDef[]> {
  const map = new Map<string, SkillDef>()
  const agentsRoot = params.agentsSkillsDir || getDefaultAgentsSkillsDir()
  const userFactoryRoot = params.userFactorySkillsDir || getDefaultUserFactorySkillsDir()
  const projectRoot = params.projectDir && params.projectDir.trim()
    ? getProjectSkillsDir(params.projectDir.trim())
    : ''

  // Precedence: project overrides ~/.factory overrides ~/.agents
  for (const def of await scanRoot(agentsRoot, 'user')) map.set(def.name, def)
  for (const def of await scanRoot(userFactoryRoot, 'user')) map.set(def.name, def)
  if (projectRoot) {
    for (const def of await scanRoot(projectRoot, 'project')) map.set(def.name, def)
  }

  const defs = Array.from(map.values())
  defs.sort((a, b) => a.name.localeCompare(b.name))
  return defs
}

