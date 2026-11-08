import { afterEach, describe, expect, test } from 'bun:test'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
import { contextPrompt } from '../src/extensions/resources.ts'
import { expandSlash, parseCommandArgs, substituteArgs } from '../src/resources/expand.ts'

/** What the files around a conversation put in front of the model: the prompt sections and the slash commands. */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

function write(under: string, path: string, body: string): void {
  const full = join(under, path)
  mkdirSync(join(full, '..'), { recursive: true })
  writeFileSync(full, body)
}

type Seen = { system: string; user: string }

/** A kernel whose model remembers what each request said, and answers it. */
async function boot(dir: string): Promise<{
  kernel: KernelHandle
  client: Client
  faux: Faux
  seen: Seen[]
}> {
  const faux = fauxCatalog()
  const seen: Seen[] = []
  const answer = (context: {
    systemPrompt?: string
    messages: { role: string; content: unknown }[]
  }) => {
    const last = [...context.messages].reverse().find((message) => message.role === 'user')
    const content = last?.content
    const user =
      typeof content === 'string'
        ? content
        : ((content as { type: string; text?: string }[] | undefined) ?? [])
            .filter((block) => block.type === 'text')
            .map((block) => block.text ?? '')
            .join('')
    // Pi sends the sections as system messages, a later one carrying only what changed.
    const sections: Record<string, string> = {}
    for (const message of context.messages as {
      role: string
      sections?: Record<string, string>
    }[]) {
      if (message.role === 'system') Object.assign(sections, message.sections ?? {})
    }
    seen.push({ system: Object.values(sections).join('\n'), user })
    return fauxAssistantMessage('noted')
  }
  faux.faux.setResponses(Array.from({ length: 20 }, () => answer as never))
  const kernel = await start({ dir, faux })
  const client = new Client(kernel.port)
  let stopped = false
  cleanups.push(async () => {
    if (stopped) return
    stopped = true
    client.close()
    await kernel.stop()
  })
  return {
    kernel,
    client,
    faux,
    seen,
  }
}

async function ask(client: Client, id: number, seen: Seen[], input: string): Promise<Seen> {
  const before = seen.length
  await client.call('conversation.submit', { id, input })
  for (let i = 0; i < 400 && seen.length === before; i++) await Bun.sleep(10)
  expect(seen.length).toBeGreaterThan(before)
  // Let the run settle before the next submission.
  for (let i = 0; i < 400; i++) {
    const page = (await client.call('conversation.entries', { id, limit: 5 }))['items'] as {
      kind: string
    }[]
    if (page.some((entry) => entry.kind === 'pi.assistant')) break
    await Bun.sleep(10)
  }
  await Bun.sleep(50)
  return seen[seen.length - 1]!
}

describe('the prompt sections', () => {
  test('carry the skills the model may load and the AGENTS.md chain, as the files are now', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const user = join(data.dir, 'user')
    const workspace = join(data.dir, 'repo')
    write(
      user,
      'skills/review/SKILL.md',
      '---\nname: review\ndescription: Review the change.\n---\nRead the diff.\n',
    )
    write(
      user,
      'skills/secret/SKILL.md',
      '---\nname: secret\ndescription: Only by name.\ndisable-model-invocation: true\n---\n',
    )
    write(user, 'skills/off/SKILL.md', '---\nname: off\ndescription: Turned off.\n---\n')
    write(
      workspace,
      '.factory/skills/local/SKILL.md',
      '---\nname: local\ndescription: This repository only.\n---\n',
    )
    write(user, 'AGENTS.md', 'Answer in English.\n')
    write(workspace, 'AGENTS.md', 'Run the tests first.\n')
    const { client, faux, seen } = await boot(data.dir)
    await client.call('skills.setDisabled', { name: 'off', disabled: true })
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: workspace },
    })
    const id = created['conversationId'] as number

    const first = await ask(client, id, seen, 'hi')
    expect(first.system).toContain('<skills>')
    expect(first.system).toContain('<name>review</name>')
    expect(first.system).toContain('<name>local</name>')
    expect(first.system).toContain(join(user, 'skills/review/SKILL.md'))
    expect(first.system).not.toContain('<name>secret</name>')
    expect(first.system).not.toContain('<name>off</name>')
    expect(first.system).toContain('<project-context>')
    expect(first.system.indexOf('Answer in English.')).toBeLessThan(
      first.system.indexOf('Run the tests first.'),
    )
    expect(first.system).toContain(`## ${join(workspace, 'AGENTS.md')}`)

    write(workspace, 'AGENTS.md', 'Run the linter first.\n')
    const second = await ask(client, id, seen, 'again')
    expect(second.system).toContain('Run the linter first.')
    expect(second.system).not.toContain('Run the tests first.')
  }, 60_000)

  test('a long chain is cut at the limit, and what is past it is named', () => {
    const files = [
      { path: '/a/AGENTS.md', bytes: 6, text: 'aaaaaa' },
      { path: '/a/b/AGENTS.md', bytes: 9, text: '一二三' },
      { path: '/a/b/c/AGENTS.md', bytes: 3, text: 'ccc' },
    ]
    const text = contextPrompt(files, 10)!
    expect(text).toContain('aaaaaa')
    // Four bytes are left for a file of three-byte characters: one character, never half of one.
    expect(text).toContain(
      '## /a/b/AGENTS.md\n\n一\n\n[6 more bytes of /a/b/AGENTS.md were left out',
    )
    expect(text).not.toContain('\uFFFD')
    expect(text).toContain('Left out, past 10 bytes: /a/b/c/AGENTS.md')
    expect(contextPrompt([])).toBeUndefined()
  })
})

describe('slash commands', () => {
  test('expand a prompt template or a skill, and leave anything else as typed', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const user = join(data.dir, 'user')
    const workspace = join(data.dir, 'repo')
    write(
      workspace,
      '.factory/prompts/fix.md',
      '---\ndescription: Fix it.\n---\nFix $1 in ${2:-the app}, then say $ARGUMENTS.\n',
    )
    write(
      user,
      'skills/review/SKILL.md',
      '---\nname: review\ndescription: Review.\n---\nRead the diff.\n',
    )
    write(user, 'skills/off/SKILL.md', '---\nname: off\ndescription: Off.\n---\nNever.\n')
    const { client, faux, seen } = await boot(data.dir)
    await client.call('skills.setDisabled', { name: 'off', disabled: true })
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: workspace },
    })
    const id = created['conversationId'] as number

    expect((await ask(client, id, seen, '/fix "the login"')).user).toBe(
      'Fix the login in the app, then say the login.\n',
    )
    const skill = (await ask(client, id, seen, '/review now please')).user
    expect(skill).toContain(
      `<skill name="review" location="${join(user, 'skills/review/SKILL.md')}">`,
    )
    expect(skill).toContain('Read the diff.')
    expect(skill.endsWith('now please')).toBe(true)
    expect((await ask(client, id, seen, '/usr/bin is where it lives')).user).toBe(
      '/usr/bin is where it lives',
    )
    expect((await ask(client, id, seen, '/off')).user).toBe('/off')
  }, 60_000)

  test('arguments split like a shell and fill the placeholders', () => {
    expect(parseCommandArgs(`a "b c" '' d`)).toEqual(['a', 'b c', '', 'd'])
    expect(substituteArgs('$1|$2|$@|${3:-z}|${@:2}|${@:1:1}', ['x', 'y'])).toBe('x|y|x y|z|y|x')
    expect(substituteArgs('$1', ['$2', 'no'])).toBe('$2')
    expect(expandSlash('/missing', { prompts: [], skills: [] }, new Set())).toBeUndefined()
  })
})

describe('skills.setDisabled', () => {
  test('is a setting: it outlives a restart and the index reset', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    write(
      join(data.dir, 'user'),
      'skills/review/SKILL.md',
      '---\nname: review\ndescription: Review.\n---\n',
    )
    let { kernel, client } = await boot(data.dir)
    const answer = await client.call('skills.setDisabled', { name: 'review', disabled: true })
    expect((answer['skills'] as { name: string; enabled: boolean }[])[0]).toMatchObject({
      name: 'review',
      enabled: false,
    })
    client.close()
    await kernel.stop()
    const db = new BunSqliteDatabase(join(data.dir, 'kernel.sqlite'))
    await db.run("DELETE FROM droi_meta WHERE key = 'index.version'")
    await db.close()
    ;({ kernel, client } = await boot(data.dir))
    const after = await client.call('skills.list', {})
    expect((after['skills'] as { enabled: boolean }[])[0]?.enabled).toBe(false)
    await client.call('skills.setDisabled', { name: 'review', disabled: false })
    expect(
      ((await client.call('skills.list', {}))['skills'] as { enabled: boolean }[])[0]?.enabled,
    ).toBe(true)
  }, 60_000)

  test('needs a name and a flag', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const { client } = await boot(data.dir)
    expect((await client.fails('skills.setDisabled', { disabled: true })).code).toBe(-32602)
    expect((await client.fails('skills.setDisabled', { name: 'x' })).code).toBe(-32602)
  }, 60_000)
})
