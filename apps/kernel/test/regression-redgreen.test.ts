// The second review's corrected red-green cases, kept as regressions. Written by the reviewer of PR
// #227: these are the ones the first round's tests claimed to cover and did not.
import { test, expect } from 'bun:test'
import { spawn } from 'node:child_process'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, TOKEN, start, fauxCatalog, tempData } from './helpers.ts'

test('corrected P1-1: SIGKILL before the index timer retains the conversation', async () => {
  const data = tempData()
  const main = new URL('../src/main.ts', import.meta.url).pathname
  const child = spawn(process.execPath, [main, '--data', data.dir], {
    env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN },
  })
  const exited = new Promise((resolve) => child.on('exit', resolve))
  let log = ''
  child.stderr.on('data', (chunk) => {
    log += chunk.toString()
  })
  let first: Client | undefined
  let kernel: Awaited<ReturnType<typeof start>> | undefined
  let second: Client | undefined
  try {
    const port = await new Promise<number>((resolve, reject) => {
      let out = ''
      const timer = setTimeout(() => reject(new Error(log)), 10000)
      child.stdout.on('data', (chunk) => {
        out += chunk.toString()
        if (!out.includes('\n')) return
        clearTimeout(timer)
        resolve(JSON.parse(out.split('\n')[0]!).port)
      })
    })
    first = new Client(port)
    const created = await first.call('conversation.create', {})
    child.kill('SIGKILL')
    await exited
    first.close()
    kernel = await start({ dir: data.dir, faux: fauxCatalog() })
    second = new Client(kernel.port)
    const list = await second.call('conversation.list')
    console.log('REAL_CRASH_LIST', list)
    expect((list.items as any[]).map((x) => x.id)).toContain(created.conversationId)
  } finally {
    first?.close()
    second?.close()
    child.kill('SIGKILL')
    await kernel?.stop()
    data.cleanup()
  }
}, 20000)

test('corrected P1-6: unwatch DURING the active run must broadcast its end', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const live = new Client(kernel.port)
  const watcher = new Client(kernel.port)
  try {
    const created = await live.call('conversation.create', { agent: { model: faux.model } })
    const id = created.conversationId
    await watcher.opened
    watcher.notifications.length = 0
    faux.faux.setResponses([
      async () => {
        await Bun.sleep(300)
        return fauxAssistantMessage('done')
      },
    ])
    await live.call('conversation.submit', { id, input: 'run first, then unwatch' })
    await watcher.waitFor(
      (n) => n.method === 'conversation.busy' && n.params.id === id && n.params.run !== null,
    )
    watcher.notifications.length = 0
    await live.call('conversation.open', { id })
    await live.call('conversation.unwatch', { id })
    const idle = await watcher.waitFor(
      (n) => n.method === 'conversation.busy' && n.params.id === id && n.params.run === null,
      1500,
    )
    expect(idle.params.run).toBeNull()
  } finally {
    live.close()
    watcher.close()
    await kernel.stop()
    data.cleanup()
  }
}, 10000)

test('corrected P1-5 create conflict checks cwd rather than only kind', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const live = new Client(kernel.port)
  try {
    await live.call('conversation.create', {
      requestId: 'cwd-conflict',
      kind: 'chat',
      agent: { model: faux.model, cwd: '/tmp/a' },
    })
    const conflict = await live.fails('conversation.create', {
      requestId: 'cwd-conflict',
      kind: 'chat',
      agent: { model: faux.model, cwd: '/tmp/b' },
    })
    expect(conflict.code).toBe(-32013)
  } finally {
    live.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('K2 in isolation missing at is invalid params', async () => {
  const data = tempData()
  const kernel = await start({ dir: data.dir, faux: fauxCatalog() })
  const live = new Client(kernel.port)
  try {
    const created = await live.call('conversation.create', {})
    const invalid = await live.fails('conversation.fork', { id: created.conversationId })
    console.log('MISSING_AT_CODE', invalid.code)
    expect(invalid.code).toBe(-32602)
  } finally {
    live.close()
    await kernel.stop()
    data.cleanup()
  }
})
