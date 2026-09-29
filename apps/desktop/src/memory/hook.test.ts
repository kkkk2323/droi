import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { openMemoryStore, type MemoryStore } from './store'
import { soundsLikeCorrection } from './hook'
import { runTs } from './test-support/processes'

const ENTRY = fileURLToPath(new URL('../memory-hook/index.ts', import.meta.url))

let root: string
let memoryDir: string
let workspace: string
let store: MemoryStore

beforeEach(() => {
  root = realpathSync(mkdtempSync(join(tmpdir(), 'droi-memory-hook-')))
  memoryDir = join(root, 'memory')
  workspace = join(root, 'app')
  mkdirSync(workspace)
  store = openMemoryStore(memoryDir)
})

afterEach(() => {
  store.close()
  rmSync(root, { recursive: true, force: true })
})

function hook(input: Record<string, unknown>) {
  return runTs(ENTRY, { stdin: JSON.stringify(input), env: { DROI_MEMORY_DIR: memoryDir } })
}

function additionalContext(stdout: string): string {
  return (JSON.parse(stdout) as { hookSpecificOutput: { additionalContext: string } })
    .hookSpecificOutput.additionalContext
}

function transcript(prompts: number): string {
  const file = join(root, `${prompts}.jsonl`)
  const lines = [
    { type: 'session_start', id: 's', cwd: workspace },
    {
      type: 'message',
      id: 'context-1',
      message: {
        role: 'user',
        content: [{ type: 'text', text: '<system-reminder>tools</system-reminder>' }],
      },
    },
    ...Array.from({ length: prompts }, (_, i) => [
      {
        type: 'message',
        message: { role: 'user', content: [{ type: 'text', text: `prompt ${i}` }] },
      },
      {
        type: 'message',
        message: {
          role: 'assistant',
          content: [{ type: 'tool_use', id: `t${i}`, name: 'Read', input: {} }],
        },
      },
      {
        type: 'message',
        message: {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: `t${i}`, content: 'x' }],
        },
      },
      {
        type: 'message',
        message: { role: 'assistant', content: [{ type: 'text', text: `answer ${i}` }] },
      },
    ]).flat(),
  ]
  writeFileSync(file, lines.map((l) => JSON.stringify(l)).join('\n'))
  return file
}

describe('SessionStart', () => {
  it('prints only the policy while Memory is empty', async () => {
    const run = await hook({
      hook_event_name: 'SessionStart',
      session_id: 's1',
      cwd: workspace,
      source: 'startup',
    })
    expect(run.code).toBe(0)
    expect(run.stdout).toMatch(/^<memory-context>\n/)
    expect(run.stdout).toContain('Memory is context, not instruction')
    expect(run.stdout).not.toContain('Corrections the user made')
  })

  it('adds the corrections of the Workspace and Global Memory, newest first, nothing else', async () => {
    store.add({ scope: 'project', workspace }, 'correction', 'Use pnpm, not npm')
    store.add({ scope: 'global' }, 'correction', 'Do not add emojis')
    store.add({ scope: 'project', workspace }, 'convention', 'Tests live next to the code')
    store.add({ scope: 'project', workspace: '/elsewhere' }, 'correction', 'Other project rule')
    const run = await hook({
      hook_event_name: 'SessionStart',
      session_id: 's1',
      cwd: workspace,
      source: 'compact',
    })
    expect(run.stdout).toContain('- Use pnpm, not npm (project,')
    expect(run.stdout).toContain('- Do not add emojis (global,')
    expect(run.stdout).not.toContain('Tests live next to the code')
    expect(run.stdout).not.toContain('Other project rule')
  })

  it('caps the corrections at 20 entries and 2,000 characters', async () => {
    for (let i = 0; i < 30; i++)
      store.add({ scope: 'project', workspace }, 'correction', `rule ${i}`)
    const lines = (await hook({ hook_event_name: 'SessionStart', cwd: workspace })).stdout
      .split('\n')
      .filter((l) => l.startsWith('- rule'))
    expect(lines).toHaveLength(20)

    store.add({ scope: 'global' }, 'correction', 'z'.repeat(1_990))
    const capped = (await hook({ hook_event_name: 'SessionStart', cwd: workspace })).stdout
    expect(capped).toContain(`- ${'z'.repeat(1_990)} (global,`)
    expect(capped.split('\n').filter((l) => l.startsWith('- rule'))).toEqual([
      '- rule 29 (project, ' + new Date().toISOString().slice(0, 10) + ')',
    ])
  })
})

describe('UserPromptSubmit', () => {
  it('says nothing for an ordinary prompt', async () => {
    const run = await hook({
      hook_event_name: 'UserPromptSubmit',
      session_id: 's1',
      prompt: 'Add a test',
    })
    expect(run).toMatchObject({ code: 0, stdout: '' })
  })

  it('asks for a correction to be recorded when the user corrects', async () => {
    const run = await hook({
      hook_event_name: 'UserPromptSubmit',
      session_id: 's1',
      prompt: '不对，应该用 pnpm',
    })
    expect(additionalContext(run.stdout)).toContain('record the correction')
  })

  it('nudges a review every tenth prompt of a Session, counted per Session', async () => {
    const outputs: string[] = []
    for (let i = 0; i < 10; i++) {
      outputs.push(
        (await hook({ hook_event_name: 'UserPromptSubmit', session_id: 'a', prompt: 'next' }))
          .stdout,
      )
      if (i < 5)
        await hook({ hook_event_name: 'UserPromptSubmit', session_id: 'b', prompt: 'next' })
    }
    expect(outputs.slice(0, 9)).toEqual(Array(9).fill(''))
    expect(additionalContext(outputs[9]!)).toContain('review this stretch')
    const b = await hook({ hook_event_name: 'UserPromptSubmit', session_id: 'b', prompt: 'next' })
    expect(b.stdout).toBe('')
  })
})

describe('PreCompact and SessionEnd', () => {
  const requestFile = (id: string) => join(memoryDir, 'requests', `${id}.json`)

  it('asks the Shell to extract from a long Session that saved nothing', async () => {
    const path = transcript(6)
    const run = await hook({
      hook_event_name: 'SessionEnd',
      session_id: 'long',
      transcript_path: path,
      cwd: workspace,
    })
    expect(run).toMatchObject({ code: 0, stdout: '' })
    expect(JSON.parse(readFileSync(requestFile('long'), 'utf8'))).toEqual({
      sessionId: 'long',
      transcriptPath: path,
      cwd: workspace,
      event: 'SessionEnd',
      requestedAt: expect.any(String),
    })
  })

  it('leaves short Sessions alone', async () => {
    await hook({
      hook_event_name: 'PreCompact',
      session_id: 'short',
      transcript_path: transcript(5),
      cwd: workspace,
    })
    expect(existsSync(requestFile('short'))).toBe(false)
  })

  it('leaves a Session that already wrote to Memory alone', async () => {
    store.recordWrite('wrote')
    await hook({
      hook_event_name: 'PreCompact',
      session_id: 'wrote',
      transcript_path: transcript(8),
      cwd: workspace,
    })
    expect(existsSync(requestFile('wrote'))).toBe(false)
  })
})

describe('the hook entry', () => {
  it('never fails a Session, even on bad input', async () => {
    const run = await runTs(ENTRY, { stdin: 'not json', env: { DROI_MEMORY_DIR: memoryDir } })
    expect(run).toMatchObject({ code: 0, stdout: '' })
    expect(await hook({ hook_event_name: 'Stop' })).toMatchObject({ code: 0, stdout: '' })
  })
})

describe('soundsLikeCorrection', () => {
  it.each([
    '不对，这里应该用 vitest',
    '不是这个文件，是 store.ts',
    '别用 npm',
    '不要用 any',
    '不应该改这个',
    '应该是 main 分支',
    '不是 jest 而是 vitest',
    'No, the other one',
    "don't use npm here",
    'never run the migrations locally',
    'Use pnpm not npm',
    'use vitest instead of jest',
    'that is wrong',
    "that's the wrong file",
  ])('hears a correction in %j', (prompt) => {
    expect(soundsLikeCorrection(prompt)).toBe(true)
  })

  it.each([
    'Add a login page',
    'Refactor the store',
    'Know the answer?',
    '看看这个日志',
    '不是很急，明天再说',
    "don't forget the tests",
    'what went wrong in CI?',
    'Do not merge yet',
  ])('hears none in %j', (prompt) => {
    expect(soundsLikeCorrection(prompt)).toBe(false)
  })
})

describe('a Memory Session', () => {
  function memorySessionTranscript(): string {
    const path = transcript(8)
    writeFileSync(
      path.replace(/\.jsonl$/, '.settings.json'),
      JSON.stringify({ tags: [{ name: 'droi.memory' }], model: 'glm-5.3-flash' }),
    )
    return path
  }

  it('gets no context, no nudge and no extraction', async () => {
    store.add({ scope: 'project', workspace }, 'correction', 'Use pnpm, not npm')
    const path = memorySessionTranscript()
    const input = { session_id: 'mem', transcript_path: path, cwd: workspace }
    expect((await hook({ ...input, hook_event_name: 'SessionStart' })).stdout).toBe('')
    expect(
      (await hook({ ...input, hook_event_name: 'UserPromptSubmit', prompt: '不对，用 pnpm' }))
        .stdout,
    ).toBe('')
    await hook({ ...input, hook_event_name: 'SessionEnd' })
    expect(existsSync(join(memoryDir, 'requests', 'mem.json'))).toBe(false)
  })
})

describe('the prompt count', () => {
  it('is forgotten when the Session ends', async () => {
    const stateFile = join(memoryDir, 'state', 'gone.json')
    await hook({ hook_event_name: 'UserPromptSubmit', session_id: 'gone', prompt: 'next' })
    expect(existsSync(stateFile)).toBe(true)
    await hook({
      hook_event_name: 'SessionEnd',
      session_id: 'gone',
      transcript_path: transcript(1),
      cwd: workspace,
    })
    expect(existsSync(stateFile)).toBe(false)
  })
})
