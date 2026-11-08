import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'

/**
 * `skills.list` and `resources.list` over the connection: the loader is tested on its own in
 * `resources.test.ts`, and this is the promise that a client's call reads the files as they are now.
 */

let data: ReturnType<typeof tempData>
let faux: Faux
let kernel: KernelHandle
let client: Client

beforeEach(async () => {
  data = tempData()
  faux = fauxCatalog()
  kernel = await start({ dir: data.dir, faux })
  client = new Client(kernel.port)
})

afterEach(async () => {
  client.close()
  await kernel.stop()
  data.cleanup()
})

function write(under: string, path: string, body: string): void {
  const full = join(under, path)
  mkdirSync(join(full, '..'), { recursive: true })
  writeFileSync(full, body)
}

const userDir = (): string => join(data.dir, 'user')

describe('skills.list', () => {
  test('reads the personal directory and, for a conversation, its repository', async () => {
    write(userDir(), 'skills/review/SKILL.md', '---\nname: review\ndescription: Review it.\n---\n')
    const workspace = join(data.dir, 'repo')
    write(
      workspace,
      '.factory/skills/local/SKILL.md',
      '---\nname: local\ndescription: Here only.\n---\n',
    )

    const personal = await client.call('skills.list', {})
    expect((personal['skills'] as { name: string }[]).map((skill) => skill.name)).toEqual([
      'review',
    ])

    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: workspace },
    })
    const id = created['conversationId'] as number
    const all = await client.call('skills.list', { id })
    expect(
      (all['skills'] as { name: string; scope: string }[]).map((skill) => [
        skill.name,
        skill.scope,
      ]),
    ).toEqual([
      ['review', 'personal'],
      ['local', 'workspace'],
    ])
  })

  test('a skill written after the last call is there on the next one', async () => {
    expect((await client.call('skills.list', {}))['skills']).toEqual([])
    write(userDir(), 'skills/late/SKILL.md', '---\nname: late\ndescription: Just written.\n---\n')
    expect((await client.call('skills.list', {}))['skills']).toEqual([
      {
        name: 'late',
        scope: 'personal',
        path: join(userDir(), 'skills/late/SKILL.md'),
        description: 'Just written.',
        enabled: true,
      },
    ])
  })

  test('an unknown conversation is -32000, not an empty answer', async () => {
    const failure = await client.fails('skills.list', { id: 424242 })
    expect(failure.code).toBe(-32000)
  })
})

describe('resources.list', () => {
  test('answers skills, prompts and the AGENTS.md chain', async () => {
    write(userDir(), 'AGENTS.md', 'the person\n')
    write(userDir(), 'prompts/fix.md', '---\ndescription: Fix it.\n---\n\nFix $ARGUMENTS.\n')
    write(userDir(), 'skills/review/SKILL.md', '---\nname: review\ndescription: Review it.\n---\n')
    const workspace = join(data.dir, 'repo')
    write(workspace, 'AGENTS.md', 'the repository\n')
    write(workspace, '.factory/prompts/release.md', '---\ndescription: Release it.\n---\n')

    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: workspace },
    })
    const answer = await client.call('resources.list', { id: created['conversationId'] })
    expect((answer['skills'] as { name: string }[]).map((skill) => skill.name)).toEqual(['review'])
    expect(
      (answer['prompts'] as { name: string; scope: string }[]).map((prompt) => [
        prompt.name,
        prompt.scope,
      ]),
    ).toEqual([
      ['fix', 'personal'],
      ['release', 'workspace'],
    ])
    const files = answer['contextFiles'] as { path: string; bytes: number }[]
    expect(files.map((file) => file.path)).toEqual([
      join(userDir(), 'AGENTS.md'),
      join(workspace, 'AGENTS.md'),
    ])
    // A path and a size, and not the text: a client shows where the instructions came from, it does not
    // read the file itself.
    expect(Object.keys(files[0]!).sort()).toEqual(['bytes', 'path'])
  })

  test('without an id only the personal half is read', async () => {
    write(userDir(), 'AGENTS.md', 'the person\n')
    const answer = await client.call('resources.list', {})
    expect((answer['contextFiles'] as unknown[]).length).toBe(1)
    expect(answer['prompts']).toEqual([])
  })
})

describe('harness.info', () => {
  test('reports the two capabilities these methods need', async () => {
    const info = await client.call('harness.info', {})
    const capabilities = info['capabilities'] as Record<string, boolean>
    expect(capabilities['skills']).toBe(true)
    expect(capabilities['resources']).toBe(true)
  })
})
