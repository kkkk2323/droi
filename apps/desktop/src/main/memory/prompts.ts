// The Memory Sessions' prompts. Droi ships them as app resources and copies
// them into the Memory folder on first use; from then on the copies are read,
// so a user can edit them, and "Reset prompts to default" copies them again.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export const PROMPT_FILES = {
  consolidation: 'consolidation-prompt.md',
  extraction: 'extraction-prompt.md',
} as const

export type PromptName = keyof typeof PROMPT_FILES

// Read and written rather than copied: the shipped files sit inside app.asar.
function copyPrompt(from: string, to: string): void {
  writeFileSync(to, readFileSync(from, 'utf8'))
}

export function readPrompt(memoryDir: string, defaultsDir: string, name: PromptName): string {
  const file = join(memoryDir, PROMPT_FILES[name])
  if (!existsSync(file)) {
    mkdirSync(memoryDir, { recursive: true })
    copyPrompt(join(defaultsDir, PROMPT_FILES[name]), file)
  }
  return readFileSync(file, 'utf8')
}

export function resetPrompts(memoryDir: string, defaultsDir: string): void {
  mkdirSync(memoryDir, { recursive: true })
  for (const file of Object.values(PROMPT_FILES)) {
    copyPrompt(join(defaultsDir, file), join(memoryDir, file))
  }
}
