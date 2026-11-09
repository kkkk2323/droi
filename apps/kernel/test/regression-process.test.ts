// The second review's process-level probes (a real SIGKILL, a real failed kernel), kept as
// regressions. Written by the reviewer of PR #227.
import { expect, test } from 'bun:test'
import { spawn } from 'node:child_process'
import { Client, TOKEN, fauxCatalog, start, tempData } from './helpers.ts'

async function child(script: string, dir: string) {
  const processChild = spawn(
    process.execPath,
    [
      new URL(script, import.meta.url).pathname,
      ...(script.includes('main.ts') ? ['--data', dir] : [dir]),
    ],
    {
      env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN },
    },
  )
  const exited = new Promise<number | null>((resolve) => processChild.on('exit', resolve))
  let log = ''
  processChild.stderr.on('data', (chunk) => {
    log += chunk.toString()
  })
  const port = await new Promise<number>((resolve, reject) => {
    let buffer = ''
    const timer = setTimeout(() => reject(new Error(log)), 10000)
    processChild.stdout.on('data', (chunk) => {
      buffer += chunk.toString()
      if (!buffer.includes('\n')) return
      clearTimeout(timer)
      resolve(JSON.parse(buffer.split('\n')[0]!).port)
    })
  })
  return { process: processChild, port, exited }
}

test('probe crash must retain create requestId, workspace and kind', async () => {
  const data = tempData()
  const running = await child('../src/main.ts', data.dir)
  const live = new Client(running.port)
  let kernel: Awaited<ReturnType<typeof start>> | undefined
  let reader: Client | undefined
  try {
    const params = {
      requestId: 'create-before-crash',
      kind: 'workstream',
      cwd: '/tmp/kernel-probe-cwd',
    }
    const first = await live.call('conversation.create', params)
    running.process.kill('SIGKILL')
    await running.exited
    live.close()
    kernel = await start({ dir: data.dir, faux: fauxCatalog() })
    reader = new Client(kernel.port)
    const list = await reader.call('conversation.list')
    const retry = await reader.call('conversation.create', params)
    console.log(
      'CRASH_METADATA',
      JSON.stringify({
        firstId: first.conversationId,
        items: list.items,
        retryId: retry.conversationId,
      }),
    )
    expect(retry.conversationId).toBe(first.conversationId)
    expect((list.items as any[])[0].workspace).toBe(params.cwd)
    expect((list.items as any[])[0].kind).toBe(params.kind)
  } finally {
    live.close()
    reader?.close()
    running.process.kill('SIGKILL')
    await kernel?.stop()
    data.cleanup()
  }
}, 20000)

test('probe storage failure must reject harness.info as well and exit exactly 3', async () => {
  const data = tempData()
  const running = await child('./failed-child.ts', data.dir)
  const live = new Client(running.port)
  try {
    await live.waitFor(
      (note) => note.method === 'warning' && note.params.kind === 'kernel_failed',
      15000,
    )
    const refused = await live.call('conversation.create', {}).catch((error) => error)
    const info = await live.call('harness.info').catch((error) => ({ error }))
    console.log('FAILURE_REPLIES', { createCode: refused.code, info })
    const status = await running.exited
    console.log('FAILED_PROCESS_EXIT', status)
    expect(status).toBe(3)
    expect((info as any).error?.code).toBe(-32010)
  } finally {
    live.close()
    running.process.kill('SIGKILL')
    data.cleanup()
  }
}, 25000)
