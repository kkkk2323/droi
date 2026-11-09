import { afterEach, describe, expect, test } from 'bun:test'
import { mkdirSync, realpathSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, allEntries, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelConfig } from '../src/config.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { context } from '../src/session/service.ts'
import { SUBAGENT_DONE } from '../src/extensions/subagent.ts'

/** Subagents: Factory Droid's task, task_output and task_stop on child conversations (ADR 0043). */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Booted = { kernel: KernelHandle; client: Client; faux: Faux; dir: string; userDir: string }

async function boot(overrides: Partial<KernelConfig> = {}): Promise<Booted> {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux, overrides })
  const client = new Client(kernel.port, undefined, 'window')
  cleanups.push(() => data.cleanup())
  cleanups.push(async () => {
    client.close()
    await kernel.stop()
  })
  return { kernel, client, faux, dir: realpathSync(data.dir), userDir: kernel.config.userDir }
}

async function conversation(b: Booted): Promise<number> {
  const created = await b.client.call('conversation.create', {
    agent: { model: b.faux.model, cwd: b.dir },
  })
  return created['conversationId'] as number
}

type Request = { systemPrompt?: string; messages: { role: string; content: unknown }[] }

/** The system prompt of a request, however the faux provider carries it. */
function systemOf(request: Request): string {
  return (
    (request.systemPrompt ?? '') +
    JSON.stringify(request.messages.filter((message) => message.role === 'system'))
  )
}

/** The text of a request's last message. */
function lastText(request: Request): string {
  const last = request.messages.at(-1)
  return JSON.stringify(last?.content ?? '')
}

const isChild = (request: Request) => systemOf(request).includes('You are a subagent')

/**
 * Answers in turn: the child's from `child`, the parent's from `parent`, each given how many requests of its side
 * came before. One faux queue serves every conversation, so the request says whose it is.
 */
function route(
  b: Booted,
  parent: (n: number, request: Request) => unknown,
  child: (n: number, request: Request) => unknown,
  seen?: Request[],
): void {
  let parents = 0
  let children = 0
  const answer = (request: Request) => {
    seen?.push(request)
    return isChild(request) ? child(children++, request) : parent(parents++, request)
  }
  b.faux.faux.setResponses(Array.from({ length: 40 }, () => answer) as never)
}

async function until<T>(read: () => Promise<T | undefined>, timeoutMs = 30_000): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const value = await read()
    if (value !== undefined) return value
    if (Date.now() > deadline) throw new Error('timed out')
    await Bun.sleep(25)
  }
}

type Entry = {
  id: number
  kind: string
  data?: Record<string, unknown>
  model?: { role: string; content: unknown; details?: Record<string, unknown>; isError?: boolean }[]
}

async function entries(b: Booted, id: number): Promise<Entry[]> {
  return (await allEntries(b.client, id)) as Entry[]
}

/** The text a tool result said, once there is one with this tool's details. */
async function resultOf(b: Booted, id: number, nth = 0): Promise<{ text: string; entry: Entry }> {
  return until(async () => {
    const results = (await entries(b, id)).filter((entry) => entry.kind === 'pi.tool-result')
    const entry = results[nth]
    if (entry === undefined) return undefined
    const content = entry.model?.[0]?.content as { type: string; text?: string }[]
    return { text: content.map((part) => part.text ?? '').join(''), entry }
  })
}

async function childTools(b: Booted, id: number): Promise<string[]> {
  const conv = await b.kernel.harness.conversation(id as never, context)
  return (await conv!.agent(context)).tools.map((tool) => tool.name)
}

describe('subagents', () => {
  test('task runs a droid in a child conversation the list leaves out, and answers what it said', async () => {
    const b = await boot()
    const id = await conversation(b)
    const seen: Request[] = []
    route(
      b,
      (n) =>
        n === 0
          ? fauxAssistantMessage(
              fauxToolCall('task', {
                subagent_type: 'explorer',
                description: 'Find the config',
                prompt: 'Where is the config read?',
              }),
              { stopReason: 'toolUse' },
            )
          : fauxAssistantMessage('parent done'),
      () => fauxAssistantMessage('It is read in src/config.ts:12.'),
      seen,
    )
    await b.client.call('conversation.submit', { id, input: 'find it with a subagent' })
    const { text, entry } = await resultOf(b, id)
    expect(text).toContain('It is read in src/config.ts:12.')
    const details = entry.model?.[0]?.details as Record<string, unknown>
    expect(details).toMatchObject({
      subagentType: 'explorer',
      description: 'Find the config',
      status: 'completed',
    })
    const childId = details['conversationId'] as number
    expect(text).toContain(`task_id: ${childId}`)

    // The child is the parent's: out of the list, found through its parent.
    const listed = (await b.client.call('conversation.list', {}))['items'] as { id: number }[]
    expect(listed.map((row) => row.id)).toEqual([id])
    const owned = (await b.client.call('conversation.list', { parent: id }))['items'] as {
      id: number
      kind?: string
      title?: string
    }[]
    expect(owned).toMatchObject([{ id: childId, kind: 'subagent', title: 'Find the config' }])

    // It got the prompt, the droid's instructions, and none of the tools a read-only droid goes without.
    const childRequest = seen.find(isChild)!
    expect(lastText(childRequest)).toContain('Where is the config read?')
    expect(systemOf(childRequest)).toContain('You are an explorer subagent')
    const tools = await childTools(b, childId)
    for (const gone of ['task', 'ask_user', 'write', 'edit', 'bash'])
      expect(tools).not.toContain(gone)
    expect(tools).toContain('read')
  }, 60_000)

  test("a person's droid in ~/.droi/droids is offered, and a name nobody defined is refused with the list", async () => {
    const b = await boot()
    mkdirSync(join(b.userDir, 'droids'), { recursive: true })
    writeFileSync(
      join(b.userDir, 'droids', 'reviewer.md'),
      '---\nname: reviewer\ndescription: Reviews a diff\n---\nYou review diffs and list the risks.\n',
    )
    const id = await conversation(b)
    const seen: Request[] = []
    route(
      b,
      (n) =>
        n === 0
          ? fauxAssistantMessage(
              fauxToolCall('task', { subagent_type: 'nobody', description: 'x', prompt: 'y' }),
              { stopReason: 'toolUse' },
            )
          : n === 1
            ? fauxAssistantMessage(
                fauxToolCall('task', {
                  subagent_type: 'reviewer',
                  description: 'Review',
                  prompt: 'the diff',
                }),
                { stopReason: 'toolUse' },
              )
            : fauxAssistantMessage('done'),
      () => fauxAssistantMessage('No risks.'),
      seen,
    )
    await b.client.call('conversation.submit', { id, input: 'review' })
    const refused = await resultOf(b, id, 0)
    expect(refused.text).toContain('There is no droid "nobody"')
    expect(refused.text).toContain('worker, explorer, reviewer')
    expect(refused.entry.model?.[0]?.isError).toBe(true)
    const reviewed = await resultOf(b, id, 1)
    expect(reviewed.text).toContain('No risks.')
    expect(systemOf(seen.find((request) => !isChild(request))!)).toContain(
      '- reviewer: Reviews a diff',
    )
    expect(systemOf(seen.find(isChild)!)).toContain('You review diffs and list the risks.')
  }, 60_000)

  test('resume goes on in the same child, with what it did before', async () => {
    const b = await boot()
    const id = await conversation(b)
    let childId = 0
    route(
      b,
      (n, request) => {
        if (n === 0) {
          return fauxAssistantMessage(
            fauxToolCall('task', {
              subagent_type: 'worker',
              description: 'First',
              prompt: 'step one',
            }),
            { stopReason: 'toolUse' },
          )
        }
        if (n === 1) {
          childId = Number(/task_id: (\d+)/.exec(lastText(request))?.[1])
          return fauxAssistantMessage(
            fauxToolCall('task', {
              subagent_type: 'worker',
              description: 'Second',
              prompt: 'step two',
              resume: String(childId),
            }),
            { stopReason: 'toolUse' },
          )
        }
        return fauxAssistantMessage('done')
      },
      (n) => fauxAssistantMessage(n === 0 ? 'did one' : 'did two'),
    )
    await b.client.call('conversation.submit', { id, input: 'two steps' })
    const second = await resultOf(b, id, 1)
    expect(second.text).toContain('did two')
    expect(
      Number(
        (second.entry.model?.[0]?.details as { conversationId?: number } | undefined)
          ?.conversationId,
      ),
    ).toBe(childId)
    const users = (await entries(b, childId)).filter((entry) => entry.kind === 'pi.user')
    expect(users.map((entry) => JSON.stringify(entry.model))).toEqual([
      expect.stringContaining('step one'),
      expect.stringContaining('step two'),
    ])
    const owned = (await b.client.call('conversation.list', { parent: id }))['items'] as unknown[]
    expect(owned).toHaveLength(1)
  }, 60_000)

  test('run_in_background answers at once, and the answer reaches the parent as an entry when it ends', async () => {
    const b = await boot()
    const id = await conversation(b)
    route(
      b,
      (n) =>
        n === 0
          ? fauxAssistantMessage(
              fauxToolCall('task', {
                subagent_type: 'worker',
                description: 'Slow job',
                prompt: 'do it',
                run_in_background: true,
              }),
              { stopReason: 'toolUse' },
            )
          : fauxAssistantMessage('parent goes on'),
      () => fauxAssistantMessage('background answer'),
    )
    await b.client.call('conversation.submit', { id, input: 'start it in the background' })
    const started = await resultOf(b, id)
    expect(started.text).toContain('Started in the background as task_id')
    expect(started.entry.model?.[0]?.details).toMatchObject({ background: true, status: 'running' })
    const done = await until(async () =>
      (await entries(b, id)).find((entry) => entry.kind === SUBAGENT_DONE),
    )
    expect(done.data).toMatchObject({
      status: 'completed',
      output: 'background answer',
      description: 'Slow job',
      background: true,
    })
    expect(JSON.stringify(done.model)).toContain('background answer')
  }, 60_000)

  test('task_stop stops a background subagent at work, and its end says it was stopped', async () => {
    const b = await boot()
    const id = await conversation(b)
    route(
      b,
      (n, request) => {
        if (n === 0) {
          return fauxAssistantMessage(
            fauxToolCall('task', {
              subagent_type: 'worker',
              description: 'Long job',
              prompt: 'wait',
              run_in_background: true,
            }),
            { stopReason: 'toolUse' },
          )
        }
        if (n === 1) {
          const taskId = /task_id (\d+)/.exec(lastText(request))?.[1] ?? ''
          return fauxAssistantMessage(fauxToolCall('task_stop', { task_id: taskId }), {
            stopReason: 'toolUse',
          })
        }
        return fauxAssistantMessage('stopped it')
      },
      (n) =>
        n === 0
          ? fauxAssistantMessage(fauxToolCall('bash', { command: 'sleep 30' }), {
              stopReason: 'toolUse',
            })
          : fauxAssistantMessage('never'),
    )
    await b.client.call('conversation.submit', { id, input: 'start and stop' })
    const stopped = await resultOf(b, id, 1)
    expect(stopped.text).toMatch(/was stopped|is not running/)
    const done = await until(async () =>
      (await entries(b, id)).find((entry) => entry.kind === SUBAGENT_DONE),
    )
    expect(done.data).toMatchObject({ status: 'stopped', description: 'Long job' })
  }, 60_000)

  test("task_output reads a subagent of this conversation's and refuses one of another's", async () => {
    const b = await boot()
    const id = await conversation(b)
    const other = await conversation(b)
    route(
      b,
      (n, request) => {
        if (n === 0) {
          return fauxAssistantMessage(
            fauxToolCall('task', { subagent_type: 'worker', description: 'Job', prompt: 'go' }),
            { stopReason: 'toolUse' },
          )
        }
        if (n === 1) {
          const taskId = /task_id: (\d+)/.exec(lastText(request))?.[1] ?? ''
          return fauxAssistantMessage(fauxToolCall('task_output', { task_id: taskId }), {
            stopReason: 'toolUse',
          })
        }
        return fauxAssistantMessage('done')
      },
      () => fauxAssistantMessage('the answer'),
    )
    await b.client.call('conversation.submit', { id, input: 'go' })
    const read = await resultOf(b, id, 1)
    expect(read.text).toContain('is not running')
    expect(read.text).toContain('the answer')
    const childId = Number(
      (read.entry.model?.[0]?.details as { conversationId?: number } | undefined)?.conversationId,
    )

    route(
      b,
      (n) =>
        n === 0
          ? fauxAssistantMessage(fauxToolCall('task_output', { task_id: String(childId) }), {
              stopReason: 'toolUse',
            })
          : fauxAssistantMessage('done'),
      () => fauxAssistantMessage('never'),
    )
    await b.client.call('conversation.submit', { id: other, input: 'peek' })
    const refused = await resultOf(b, other)
    expect(refused.text).toContain('There is no subagent')
  }, 60_000)

  test('droids.list and droids.set keep the droids in ~/.droi/droids', async () => {
    const b = await boot()
    const listed = await b.client.call('droids.list')
    expect((listed['droids'] as { name: string }[]).map((droid) => droid.name)).toEqual([
      'worker',
      'explorer',
    ])
    const set = await b.client.call('droids.set', {
      name: 'reviewer',
      droid: {
        name: 'reviewer',
        description: 'Reviews a diff',
        prompt: 'List the risks.',
        model: 'inherit',
        reasoningEffort: 'high',
        tools: ['read', 'grep'],
      },
    })
    expect((set['droids'] as Record<string, unknown>[]).at(-1)).toMatchObject({
      name: 'reviewer',
      description: 'Reviews a diff',
      prompt: 'List the risks.',
      reasoningEffort: 'high',
      tools: ['read', 'grep'],
      source: 'personal',
      path: join(b.userDir, 'droids', 'reviewer.md'),
    })
    // Renamed: one file, under the new name.
    await b.client.call('droids.set', {
      name: 'reviewer',
      droid: { name: 'critic', description: 'Reviews a diff', prompt: 'List the risks.' },
    })
    const renamed = (await b.client.call('droids.list'))['droids'] as { name: string }[]
    expect(renamed.map((droid) => droid.name)).toEqual(['worker', 'explorer', 'critic'])
    // A person's worker takes the built-in's place; removing it brings the built-in back.
    await b.client.call('droids.set', {
      name: 'worker',
      droid: { name: 'worker', description: 'Mine', prompt: 'My worker.' },
    })
    const mine = (await b.client.call('droids.list'))['droids'] as {
      name: string
      source: string
    }[]
    expect(mine[0]).toMatchObject({ name: 'worker', source: 'personal' })
    await b.client.call('droids.set', { name: 'worker', remove: true })
    const back = (await b.client.call('droids.list'))['droids'] as {
      name: string
      source: string
    }[]
    expect(back[0]).toMatchObject({ name: 'worker', source: 'builtin' })
    expect((await b.client.fails('droids.set', { name: 'explorer', remove: true })).code).toBe(
      -32602,
    )
    expect(
      (
        await b.client.fails('droids.set', {
          name: 'x',
          droid: { name: 'Bad Name', description: 'd', prompt: 'p' },
        })
      ).code,
    ).toBe(-32602)
    writeFileSync(
      join(b.userDir, 'droids', 'broken.md'),
      '---\nname: broken\n---\nno description\n',
    )
    const diagnostics = (await b.client.call('droids.list'))['diagnostics'] as { message: string }[]
    expect(diagnostics).toMatchObject([{ message: 'a description is required' }])
  }, 60_000)
})
