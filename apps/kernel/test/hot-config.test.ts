import { afterEach, describe, expect, test } from 'bun:test'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, waitIdle } from './helpers.ts'

/**
 * Settings that change while the kernel runs (4R, ADR 0038): `harness.configure` turns Memory on or off and replaces
 * the providers without a restart, and every connection hears `harness.changed`.
 */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

const MEMORY_TOOLS = ['memory_search', 'memory_read', 'memory_list', 'memory_note']

async function boot(memoryDir?: string) {
  const data = tempData()
  cleanups.push(() => data.cleanup())
  const faux = fauxCatalog()
  const seen: string[][] = []
  const answer = (context: {
    messages: { role: string; toolsAdded?: { name: string }[]; toolsRemoved?: { name: string }[] }[]
  }) => {
    const offered = new Set<string>()
    let system = false
    for (const message of context.messages) {
      if (message.role !== 'system') continue
      system = true
      for (const tool of message.toolsRemoved ?? []) offered.delete(tool.name)
      for (const tool of message.toolsAdded ?? []) offered.add(tool.name)
    }
    if (!system) return fauxAssistantMessage('A title')
    seen.push([...offered])
    return fauxAssistantMessage('noted')
  }
  faux.faux.setResponses(Array.from({ length: 30 }, () => answer as never))
  const kernel = await start({
    dir: data.dir,
    faux,
    ...(memoryDir === undefined ? {} : { overrides: { memoryDir } }),
  })
  const client = new Client(kernel.port)
  cleanups.push(async () => {
    client.close()
    await kernel.stop()
  })
  const id = (
    await client.call('conversation.create', { agent: { model: faux.model, cwd: data.dir } })
  )['conversationId'] as number
  const ask = async (input: string) => {
    const before = seen.length
    await client.call('conversation.submit', { id, input })
    for (let i = 0; i < 500 && seen.length === before; i++) await Bun.sleep(10)
    await waitIdle(client, id)
    return seen.at(-1)!
  }
  return { client, ask, dir: data.dir }
}

function memoryFolder(root: string): string {
  const dir = join(root, 'memory')
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, 'memory_summary.md'), 'v1\n## User Profile\nA developer.\n')
  return dir
}

describe('harness.configure', () => {
  test('turns Memory on and off for a running conversation at its next turn', async () => {
    const { client, ask, dir } = await boot()
    expect(await ask('hi')).not.toContain('memory_search')

    await client.call('harness.configure', { memoryDir: memoryFolder(dir) })
    await client.waitForMethod('harness.changed')
    expect(await ask('again')).toEqual(expect.arrayContaining(MEMORY_TOOLS))

    await client.call('harness.configure', { memoryDir: null })
    const off = await ask('and again')
    for (const tool of MEMORY_TOOLS) expect(off).not.toContain(tool)
  })

  test('takes Memory away from a kernel that started with it', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const { client, ask } = await boot(memoryFolder(data.dir))
    expect(await ask('hi')).toEqual(expect.arrayContaining(MEMORY_TOOLS))
    await client.call('harness.configure', { memoryDir: null })
    expect(await ask('again')).not.toContain('memory_note')
  })

  test('replaces the providers, and harness.info lists the new catalog', async () => {
    const { client } = await boot()
    const provider = {
      id: 'factory-anthropic',
      name: 'Factory',
      baseUrl: 'http://127.0.0.1:9/api/llm/a',
      api: 'anthropic-messages',
      apiKeyEnv: 'DROI_KERNEL_LLM_TOKEN',
      models: [{ id: 'claude-sonnet-4-5', contextWindow: 200000, maxTokens: 64000 }],
    }
    await client.call('harness.configure', { providers: [provider] })
    await client.waitForMethod('harness.changed')
    let info = await client.call('harness.info')
    expect(info['providers']).toEqual([
      { id: 'factory-anthropic', name: 'Factory', models: ['claude-sonnet-4-5'] },
    ])

    await client.call('harness.configure', {
      providers: [
        {
          ...provider,
          models: [{ id: 'claude-opus-4-1', contextWindow: 200000, maxTokens: 32000 }],
        },
      ],
    })
    info = await client.call('harness.info')
    expect((info['models'] as { id: string }[]).map((model) => model.id)).toEqual([
      'claude-opus-4-1',
    ])

    await client.call('harness.configure', { providers: [] })
    info = await client.call('harness.info')
    expect(info['providers']).toEqual([])
  })

  test('sets the model a new conversation starts with, which harness.info reports', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const faux = fauxCatalog()
    faux.faux.setResponses([fauxAssistantMessage('A title')])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    cleanups.push(async () => {
      client.close()
      await kernel.stop()
    })
    expect((await client.call('harness.info'))['agent']).toBeUndefined()

    await client.call('harness.configure', { agent: { model: faux.model, thinkingLevel: 'high' } })
    await client.waitForMethod('harness.changed')
    expect((await client.call('harness.info'))['agent']).toEqual({
      model: faux.model,
      thinkingLevel: 'high',
    })
    const created = await client.call('conversation.create', { agent: { cwd: data.dir } })
    const opened = await client.call('conversation.open', {
      id: created['conversationId'],
      seen: false,
    })
    const agent = (
      opened['view'] as { docs: Record<string, { model?: unknown; thinkingLevel?: string }> }
    ).docs['pi.agent']
    expect(agent?.model).toMatchObject(faux.model)
    expect(agent?.thinkingLevel).toBe('high')

    // A model the create names wins over it.
    const named = await client.call('conversation.create', {
      agent: { cwd: data.dir, thinkingLevel: 'low' },
    })
    const reopened = await client.call('conversation.open', {
      id: named['conversationId'],
      seen: false,
    })
    expect(
      (reopened['view'] as { docs: Record<string, { thinkingLevel?: string }> }).docs['pi.agent']
        ?.thinkingLevel,
    ).toBe('low')

    await client.call('harness.configure', { agent: null })
    expect((await client.call('harness.info'))['agent']).toBeUndefined()

    const unknown = await client.fails('harness.configure', {
      agent: { model: { provider: 'nobody', modelId: 'nothing' } },
    })
    expect(unknown.code).toBe(-32602)
    expect(
      (await client.fails('harness.configure', { agent: { thinkingLevel: 'high' } })).code,
    ).toBe(-32602)
  })

  test('refuses what it cannot take, and changes nothing then', async () => {
    const { client } = await boot()
    const bad = await client.fails('harness.configure', {
      providers: [{ id: 'x', api: 'carrier-pigeon', models: [] }],
    })
    expect(bad.code).toBe(-32602)
    expect((await client.fails('harness.configure', { memoryDir: 'memory' })).code).toBe(-32602)
    expect((await client.fails('harness.configure', { memoryDir: 7 })).code).toBe(-32602)
    expect((await client.call('harness.info'))['providers']).toEqual([])
  })
})
