import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { readPrompt, resetPrompts } from './prompts'

const DEFAULTS = fileURLToPath(new URL('../../../resources/memory', import.meta.url))

let dir: string

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'droi-memory-prompts-'))
})

afterEach(() => rmSync(dir, { recursive: true, force: true }))

describe('Memory Session prompts', () => {
  it('copies the shipped prompt on first use, then reads the copy', () => {
    const shipped = readFileSync(join(DEFAULTS, 'extraction-prompt.md'), 'utf8')
    expect(readPrompt(dir, DEFAULTS, 'extraction')).toBe(shipped)
    writeFileSync(join(dir, 'extraction-prompt.md'), 'my own prompt')
    expect(readPrompt(dir, DEFAULTS, 'extraction')).toBe('my own prompt')
    expect(readPrompt(dir, DEFAULTS, 'consolidation')).toContain('Every id you were sent')
  })

  it('resets both prompts to the shipped ones', () => {
    writeFileSync(join(dir, 'consolidation-prompt.md'), 'edited')
    resetPrompts(dir, DEFAULTS)
    expect(readPrompt(dir, DEFAULTS, 'consolidation')).toBe(
      readFileSync(join(DEFAULTS, 'consolidation-prompt.md'), 'utf8'),
    )
  })
})
