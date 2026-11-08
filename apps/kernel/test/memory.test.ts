import { afterEach, describe, expect, test } from 'bun:test'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, readdirSync, utimesSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import type { ToolRegistration } from '@earendil-works/pi-durable'
import { MEMORY_SECTION, memoryExtension } from '../src/extensions/memory.ts'
import { memoryContext, writeNote } from '../src/memory/folder.ts'
import { decide } from '../src/policy/decide.ts'
import { memoryDirFromEnv } from '../src/config.ts'
import { Client, fauxCatalog, fauxToolCall, start, tempData, waitIdle } from './helpers.ts'

/**
 * Memory in the Kernel (ADR 0042): the four tools answer as the Go Memory Server did (its server_test.go cases,
 * word for word), and a conversation's prompt holds the Memory it was first shown.
 */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

const SUMMARY =
  "v1\n## User Profile\nA developer.\n## User preferences\n- Answers in Chinese.\n## General Tips\n- Run pnpm check.\n## What's in Memory\n### /Users/dev/app\n- session_summaries/a.md — the login fix\n"

function write(dir: string, path: string, body: string, at?: Date): void {
  const full = join(dir, path)
  mkdirSync(join(full, '..'), { recursive: true })
  writeFileSync(full, body)
  // Search orders by a file's modification time: files written in the same millisecond would tie.
  if (at !== undefined) utimesSync(full, at, at)
}

/** The Go suite's Memory folder: a Memory Summary and two Session Summaries. */
function folder(): string {
  const data = tempData()
  cleanups.push(() => data.cleanup())
  const dir = join(data.dir, 'memory')
  write(dir, 'memory_summary.md', SUMMARY, new Date('2026-10-27T08:00:00Z'))
  write(
    dir,
    'session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md',
    'session_id: aaaaaaaa\nupdated_at: 2026-10-20T10:00:00Z\nworkspace: /Users/dev/app\ntitle: Fix login\n\n# Fix login\nThe user said to use pnpm, not npm.\nThe session store lives in src/session.ts.\n',
    new Date('2026-10-27T08:00:01Z'),
  )
  write(
    dir,
    'session_summaries/2026-10-26T10-00-00-bbbbbbbb-deploy.md',
    'session_id: bbbbbbbb\nupdated_at: 2026-10-26T10:00:00Z\nworkspace: /Users/dev/infra\ntitle: Deploy\n\n# Deploy\nDeploy with make ship.\n',
    new Date('2026-10-27T08:00:02Z'),
  )
  return dir
}

const NOW = new Date('2026-10-27T09:00:00Z')

/** The extension's tools on `dir`, called as conversation `s1` calls them. */
function tools(dir: string) {
  const reads: string[] = []
  const extension = memoryExtension({ dir, onRead: (file) => reads.push(file), now: () => NOW })
  const call = async (name: string, args: Record<string, unknown>) => {
    const tool = (extension.tools ?? []).find((t) => t.name === name) as ToolRegistration
    const result = (await tool.execute(
      args as never,
      { conversationId: 's1' } as never,
      {} as never,
    )) as {
      output: { text: string }[]
      isError?: boolean
    }
    return { text: result.output[0]!.text, isError: result.isError === true }
  }
  return { call, reads, extension }
}

describe('the Memory tools', () => {
  test('search wants every word first, then any', async () => {
    const { call } = tools(folder())
    expect((await call('memory_search', { query: 'pnpm npm' })).text).toBe(
      '2 lines in Memory hold every word of "pnpm npm" (read a file with memory_read):\nsession_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md:7: The user said to use pnpm, not npm.\nmemory_summary.md:7: - Run pnpm check.\n',
    )
    expect((await call('memory_search', { query: 'deploy session.ts' })).text).toStartWith(
      '4 lines in Memory hold some words of',
    )
    expect((await call('memory_search', { query: '"make ship"' })).text).toContain(
      '2026-10-26T10-00-00-bbbbbbbb-deploy.md:7: Deploy with make ship.',
    )
    expect((await call('memory_search', { query: 'kubernetes' })).text).toBe(
      'Nothing in Memory matches "kubernetes".',
    )
    expect((await call('memory_search', { query: ' ' })).isError).toBe(true)
  })

  test("read numbers the lines and counts a Session Summary's read, and nothing outside Memory", async () => {
    const { call, reads } = tools(folder())
    const got = await call('memory_read', {
      path: 'session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md',
      offset: 6,
      limit: 2,
    })
    expect(got).toEqual({
      text: 'session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md, lines 6-7 of 8:\n6: # Fix login\n7: The user said to use pnpm, not npm.\n',
      isError: false,
    })
    await call('memory_read', { path: 'memory_summary.md' })
    expect(reads).toEqual(['session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md'])
    for (const path of [
      '../../etc/passwd',
      '/etc/passwd',
      'state.json',
      'notes/../state.json',
      '',
    ]) {
      const refused = await call('memory_read', { path })
      expect(refused.isError).toBe(true)
      expect(refused.text).toStartWith('Memory has no file')
    }
  })

  test('list shows the Session Summaries newest first, and the notes', async () => {
    const dir = folder()
    const { call } = tools(dir)
    writeNote(
      dir,
      'The user asked to remember: tabs, not spaces',
      undefined,
      new Date('2026-10-27T08:00:00Z'),
    )
    expect((await call('memory_list', {})).text).toBe(
      'memory_summary.md: the Memory Summary, shown at the start of every Session.\n' +
        '2 Session Summaries, newest first:\n' +
        '- session_summaries/2026-10-26T10-00-00-bbbbbbbb-deploy.md · 2026-10-26 · /Users/dev/infra · Deploy\n' +
        '- session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md · 2026-10-20 · /Users/dev/app · Fix login\n' +
        '1 Memory Notes:\n- notes/2026-10-27T08-00-00-the-user-asked-to-remember-tabs-not.md\n',
    )
    const one = (await call('memory_list', { workspace: 'dev/app' })).text
    expect(one).toContain('1 Session Summaries')
    expect(one).not.toContain('infra')
  })

  test('note saves what the user asked for, and never a secret', async () => {
    const dir = folder()
    const { call } = tools(dir)
    const saved = await call('memory_note', {
      text: 'The user asked to forget: the old staging host',
    })
    expect(saved.isError).toBe(false)
    expect(saved.text).toStartWith(
      'Saved notes/2026-10-27T09-00-00-the-user-asked-to-forget-the-old-staging.md.',
    )
    const body = readFileSync(
      join(dir, 'notes/2026-10-27T09-00-00-the-user-asked-to-forget-the-old-staging.md'),
      'utf8',
    )
    expect(body).toBe(
      'written_at: 2026-10-27T09:00:00Z\nsession_id: s1\n\nThe user asked to forget: the old staging host\n',
    )
    // The same text at the same second is a second note, not the first one replaced.
    await call('memory_note', { text: 'The user asked to forget: the old staging host' })
    expect(readdirSync(join(dir, 'notes'))).toContain(
      '2026-10-27T09-00-00-the-user-asked-to-forget-the-old-staging-2.md',
    )
    const secret = await call('memory_note', { text: 'api_key: 0123456789abcdef0123' })
    expect(secret.isError).toBe(true)
    expect(secret.text).not.toContain('0123456789')
  })

  test('run without asking, at every level', () => {
    for (const name of ['memory_search', 'memory_read', 'memory_list', 'memory_note']) {
      expect(decide({ name }, { autonomy: 'ask', rules: [] }, undefined)).toEqual({
        verdict: 'allow',
      })
    }
  })
})

describe('DROI_MEMORY_DIR', () => {
  test('is Memory on that folder; unset or blank is Memory off, and a relative path is refused', () => {
    expect(memoryDirFromEnv({ DROI_MEMORY_DIR: '/tmp/memory' } as never)).toBe('/tmp/memory')
    expect(memoryDirFromEnv({} as never)).toBeUndefined()
    expect(memoryDirFromEnv({ DROI_MEMORY_DIR: '  ' } as never)).toBeUndefined()
    expect(() => memoryDirFromEnv({ DROI_MEMORY_DIR: 'memory' } as never)).toThrow()
  })
})

describe("a conversation's Memory", () => {
  test('is the read policy, the Memory Summary and the notes the summary has not taken in', () => {
    const dir = folder()
    const taken = writeNote(
      dir,
      'The user asked to remember: tabs',
      undefined,
      new Date('2026-10-27T08:00:00Z'),
    )
    writeNote(
      dir,
      'The user asked to remember: deploys on Fridays are fine',
      undefined,
      new Date('2026-10-27T08:01:00Z'),
    )
    const path = (taken as { path: string }).path
    const hash = createHash('sha256')
      .update(readFileSync(join(dir, path)))
      .digest('hex')
    writeFileSync(join(dir, 'state.json'), JSON.stringify({ phase2: { inputs: { [path]: hash } } }))
    const text = memoryContext(dir)
    expect(text).toStartWith('<memory-context>\n## Memory\n')
    expect(text).toContain(
      `========= MEMORY_SUMMARY BEGINS =========\n${SUMMARY.trim()}\n========= MEMORY_SUMMARY ENDS =========\n`,
    )
    expect(text).toContain(
      'does not include yet, newest first; they win over it:\n- The user asked to remember: deploys on Fridays are fine\n</memory-context>',
    )
    expect(text).not.toContain('remember: tabs')
    expect(text).toEndWith('</memory-context>\n')
  })

  test('before any Memory Summary says why there is none', () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    expect(memoryContext(join(data.dir, 'nothing'))).toContain(
      'There is no Memory Summary yet: Droi writes it from Sessions once they have been idle for a while.',
    )
  })

  type Seen = { messages: unknown[]; tools: string[]; memory: string | undefined }

  /** A kernel with Memory on `dir` (or off), whose model remembers every request and answers from `script`. */
  async function boot(dir: string | undefined, script: ((seen: Seen) => unknown)[] = []) {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const faux = fauxCatalog()
    const seen: Seen[] = []
    let next = 0
    const answer = (context: {
      messages: {
        role: string
        sections?: Record<string, string>
        toolsAdded?: { name: string }[]
        toolsRemoved?: { name: string }[]
      }[]
    }) => {
      // Pi sends the sections and the tools as system messages, a later one carrying only what changed.
      const sections: Record<string, string> = {}
      const offered = new Set<string>()
      for (const message of context.messages) {
        if (message.role !== 'system') continue
        Object.assign(sections, message.sections ?? {})
        for (const tool of message.toolsRemoved ?? []) offered.delete(tool.name)
        for (const tool of message.toolsAdded ?? []) offered.add(tool.name)
      }
      const record = {
        messages: JSON.parse(JSON.stringify(context.messages)) as unknown[],
        tools: [...offered],
        memory: sections[MEMORY_SECTION],
      }
      seen.push(record)
      const step = script[next++]
      return step === undefined ? fauxAssistantMessage('noted') : step(record)
    }
    faux.faux.setResponses(Array.from({ length: 30 }, () => answer as never))
    const kernel = await start({
      dir: data.dir,
      faux,
      ...(dir === undefined ? {} : { overrides: { memoryDir: dir } }),
    })
    const client = new Client(kernel.port)
    cleanups.push(async () => {
      client.close()
      await kernel.stop()
    })
    const create = async () =>
      (await client.call('conversation.create', { agent: { model: faux.model, cwd: data.dir } }))[
        'conversationId'
      ] as number
    const ask = async (id: number, input: string) => {
      const before = seen.length
      await client.call('conversation.submit', { id, input })
      for (let i = 0; i < 500 && seen.length === before; i++) await Bun.sleep(10)
      await waitIdle(client, id)
    }
    return { client, seen, create, ask }
  }

  test('is shown once and kept: a note written meanwhile leaves the prompt as it was, and the next conversation has it', async () => {
    const dir = folder()
    const { seen, create, ask } = await boot(dir)
    const id = await create()
    await ask(id, 'hi')
    const first = seen.at(-1)!
    expect(first.memory).toContain('========= MEMORY_SUMMARY BEGINS =========')
    expect(first.tools).toEqual(
      expect.arrayContaining(['memory_search', 'memory_read', 'memory_list', 'memory_note']),
    )

    writeNote(dir, 'The user asked to remember: deploys on Fridays are fine', undefined, new Date())
    write(dir, 'memory_summary.md', SUMMARY.replace('A developer.', 'A changed developer.'))
    await ask(id, 'again')
    const second = seen.at(-1)!
    // The provider's prompt cache holds: everything the first request sent comes again, byte for byte.
    expect(JSON.stringify(second.messages.slice(0, first.messages.length))).toBe(
      JSON.stringify(first.messages),
    )
    expect(second.memory).toBe(first.memory)
    expect(second.memory).not.toContain('Fridays')

    const other = await create()
    await ask(other, 'hi')
    const fresh = seen.at(-1)!
    expect(fresh.memory).toContain('- The user asked to remember: deploys on Fridays are fine')
    expect(fresh.memory).toContain('A changed developer.')
  }, 60_000)

  test('a Session Summary the model reads is told to the Host; a note it writes names the conversation', async () => {
    const dir = folder()
    const { client, seen, create, ask } = await boot(dir, [
      () =>
        fauxAssistantMessage(
          fauxToolCall('memory_read', {
            path: 'session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md',
          }),
          { stopReason: 'toolUse' },
        ),
      () => fauxAssistantMessage('read it'),
      () =>
        fauxAssistantMessage(
          fauxToolCall('memory_note', { text: 'The user asked to remember: tabs' }),
          {
            stopReason: 'toolUse',
          },
        ),
      () => fauxAssistantMessage('saved'),
    ])
    const id = await create()
    await ask(id, 'what did we do about login?')
    const read = await client.waitFor((note) => note.method === 'memory.read')
    expect(read.params['file']).toBe('session_summaries/2026-10-20T10-00-00-aaaaaaaa-login.md')
    expect(new Date(read.params['at'] as string).toISOString()).toBe(read.params['at'] as string)
    expect(JSON.stringify(seen.at(-1)!.messages)).toContain('The user said to use pnpm, not npm.')

    await ask(id, 'remember tabs')
    const notes = readdirSync(join(dir, 'notes'))
    expect(notes).toHaveLength(1)
    expect(readFileSync(join(dir, 'notes', notes[0]!), 'utf8')).toContain(`session_id: ${id}\n`)
  }, 60_000)

  test('with Memory off a conversation has no Memory and no Memory tools', async () => {
    const { seen, create, ask } = await boot(undefined)
    await ask(await create(), 'hi')
    expect(seen.at(-1)!.memory).toBeUndefined()
    expect(seen.at(-1)!.tools).toContain('read')
    expect(seen.at(-1)!.tools.some((name) => name.startsWith('memory_'))).toBe(false)
  }, 60_000)
})
