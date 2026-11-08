import { defineExtension, section } from '@earendil-works/pi-durable'
import type { Extension } from '@earendil-works/pi-durable'
import {
  loadContextChain,
  loadSkills,
  type ContextFileInfo,
  type SkillInfo,
} from '../resources/index.ts'

/** How much of the `AGENTS.md` chain the prompt carries; what is past it is named, not sent. */
export const CONTEXT_LIMIT = 32 * 1024

export type ResourcesOptions = {
  /** The user's own directory: personal skills and `AGENTS.md`. */
  userDir: string
  /** The skills a person turned off, read at each request. */
  disabled: () => ReadonlySet<string>
}

/**
 * The files around a conversation, in its system prompt: the skills it may load and the `AGENTS.md` chain of
 * its working directory. Sections render before each request, so a file a person edits is in the next one.
 */
export function resourcesExtension(options: ResourcesOptions): Extension {
  return defineExtension({
    name: 'resources',
    sections: [
      section('skills', ({ agent }) => {
        const disabled = options.disabled()
        const skills = loadSkills({ userDir: options.userDir, workspace: agent.cwd }).filter(
          (skill) => !skill.hidden && !disabled.has(skill.name),
        )
        return skillsPrompt(skills)
      }),
      section('project-context', ({ agent }) =>
        contextPrompt(loadContextChain({ userDir: options.userDir, workspace: agent.cwd })),
      ),
    ],
  })
}

/** The skills the model may load, in the words Pi's coding agent uses (`formatSkillsForPrompt`). */
export function skillsPrompt(skills: readonly SkillInfo[]): string | undefined {
  if (skills.length === 0) return undefined
  const lines = [
    'The following skills provide specialized instructions for specific tasks.',
    "Use the read tool to load a skill's file when the task matches its description.",
    "Resolve relative paths against the skill's directory (parent of SKILL.md). Use absolute paths in tool calls.",
    '',
    '<available_skills>',
  ]
  for (const skill of skills) {
    lines.push('  <skill>')
    lines.push(`    <name>${escapeXml(skill.name)}</name>`)
    lines.push(`    <description>${escapeXml(skill.description)}</description>`)
    lines.push(`    <location>${escapeXml(skill.path)}</location>`)
    lines.push('  </skill>')
  }
  lines.push('</available_skills>')
  return lines.join('\n')
}

/**
 * The `AGENTS.md` chain, broadest first, each file under its path, up to `limit` bytes of their text. A file
 * that does not fit is cut where the limit falls, and every file after it is named as left out.
 */
export function contextPrompt(
  files: readonly ContextFileInfo[],
  limit = CONTEXT_LIMIT,
): string | undefined {
  if (files.length === 0) return undefined
  const parts = [
    'Instructions from the AGENTS.md files that apply to the working directory, broadest first: a later one can',
    'override an earlier one.',
  ]
  let left = limit
  const omitted: string[] = []
  for (const file of files) {
    if (left <= 0) {
      omitted.push(file.path)
      continue
    }
    const bytes = Buffer.from(file.text, 'utf8')
    let text = file.text.trim()
    if (bytes.length > left) {
      // Cut on a character boundary: decoding a slice that ends inside one gives a replacement character.
      const kept = new TextDecoder().decode(bytes.subarray(0, left)).replace(/\uFFFD+$/, '')
      const dropped = bytes.length - Buffer.byteLength(kept, 'utf8')
      text = `${kept.trimEnd()}\n\n[${dropped} more bytes of ${file.path} were left out: the instructions are over ${limit} bytes.]`
    }
    left -= bytes.length
    parts.push('', `## ${file.path}`, '', text)
  }
  if (omitted.length > 0) {
    parts.push(
      '',
      `Left out, past ${limit} bytes: ${omitted.join(', ')}. Read them if the task needs them.`,
    )
  }
  return parts.join('\n')
}

function escapeXml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}
