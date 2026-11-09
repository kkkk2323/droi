import { dirname } from 'node:path'
import { skillBody, type PromptInfo, type SkillInfo } from './index.ts'

/**
 * A slash command, expanded the way Pi's coding agent expands it: `/name args` names a prompt template, whose
 * `$1`, `$@`, `$ARGUMENTS` and their `${…}` forms take the arguments, or a skill, whose instructions are put
 * in the message. Prompts are looked up first. Anything else is not a command: `/usr/bin` is a path a person
 * typed, and goes as it was. Ported from pi-coding-agent 1.5.0 (`core/prompt-templates.ts` parseCommandArgs,
 * substituteArgs, expandPromptTemplate; `core/agent-session.ts` _expandSkillCommand).
 */
export function expandSlash(
  text: string,
  resources: { prompts: readonly PromptInfo[]; skills: readonly SkillInfo[] },
  disabled: ReadonlySet<string>,
): string | undefined {
  const match = /^\/([^\s]+)(?:\s+([\s\S]*))?$/.exec(text)
  if (match === null) return undefined
  const name = match[1]!
  const rest = match[2] ?? ''
  const prompt = resources.prompts.find((candidate) => candidate.name === name)
  if (prompt !== undefined) return substituteArgs(prompt.body, parseCommandArgs(rest))
  const skill = resources.skills.find((candidate) => candidate.name === name)
  if (skill === undefined || disabled.has(skill.name)) return undefined
  const body = skillBody(skill.path)
  if (body === undefined) return undefined
  const block = [
    `<skill name="${skill.name}" location="${skill.path}">`,
    `References are relative to ${dirname(skill.path)}.`,
    '',
    body.trim(),
    '</skill>',
  ].join('\n')
  const args = rest.trim()
  return args === '' ? block : `${block}\n\n${args}`
}

/** Arguments split as a shell would split them: on whitespace, with quotes holding a group together. */
export function parseCommandArgs(text: string): string[] {
  const args: string[] = []
  let current = ''
  let started = false
  let quote: string | undefined
  for (const char of text) {
    if (quote !== undefined) {
      if (char === quote) quote = undefined
      else current += char
    } else if (char === '"' || char === "'") {
      quote = char
      started = true
    } else if (/\s/.test(char)) {
      if (started) {
        args.push(current)
        current = ''
        started = false
      }
    } else {
      current += char
      started = true
    }
  }
  if (started) args.push(current)
  return args
}

/**
 * `$1`…`$n`, `$@` and `$ARGUMENTS`, `${N:-default}`, `${@:-default}`, `${@:N}` and `${@:N:L}`. Only the
 * template is read for placeholders: an argument that holds `$1` stays as it was.
 */
export function substituteArgs(content: string, args: readonly string[]): string {
  const all = args.join(' ')
  return content.replace(
    /\$\{(\d+|ARGUMENTS|@):-([^}]*)\}|\$\{@:(\d+)(?::(\d+))?\}|\$(ARGUMENTS|@|\d+)/g,
    (
      _match,
      defaultTarget: string | undefined,
      defaultValue: string | undefined,
      sliceStart: string | undefined,
      sliceLength: string | undefined,
      simple: string | undefined,
    ) => {
      if (defaultTarget !== undefined) {
        const value =
          defaultTarget === '@' || defaultTarget === 'ARGUMENTS'
            ? all
            : args[Number(defaultTarget) - 1]
        return value !== undefined && value !== '' ? value : (defaultValue ?? '')
      }
      if (sliceStart !== undefined) {
        const start = Math.max(Number(sliceStart) - 1, 0)
        return (
          sliceLength === undefined
            ? args.slice(start)
            : args.slice(start, start + Number(sliceLength))
        ).join(' ')
      }
      if (simple === 'ARGUMENTS' || simple === '@') return all
      return args[Number(simple) - 1] ?? ''
    },
  )
}
