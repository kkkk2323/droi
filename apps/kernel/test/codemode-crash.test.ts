import { afterEach, describe, expect, test } from 'bun:test'
import { mkdtempSync, readFileSync, realpathSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'

/**
 * kill -9 in the middle of a codemode script (design/m3-kernel.md §6.4): the next start finishes the call, its
 * cut-off nested call says so, and no task is left behind.
 */

const childPath = new URL('./codemode-crash-child.ts', import.meta.url).pathname
const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Entry = {
  kind: string
  model?: {
    isError?: boolean
    details?: { calls?: { name: string; status: string; output?: string }[] }
  }[]
}

describe('a kernel killed mid-script', () => {
  test('finishes the codemode call on the next start, and leaves no task', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const work = realpathSync(mkdtempSync(join(tmpdir(), 'codemode-crash-')))
    cleanups.push(() => rmSync(work, { recursive: true, force: true }))
    const child = Bun.spawn([process.execPath, 'run', childPath, data.dir, work], {
      stdout: 'pipe',
      stderr: 'pipe',
    })
    const line = await (async () => {
      const reader = child.stdout.getReader()
      let buffer = ''
      for (;;) {
        const { value, done } = await reader.read()
        if (done) throw new Error(await new Response(child.stderr).text())
        buffer += new TextDecoder().decode(value)
        const found = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
        if (found !== undefined) return found
      }
    })()
    const { conversationId } = JSON.parse(line) as { conversationId: number }
    const sleeper = Number(readFileSync(join(work, 'sleep.pid'), 'utf8').trim())
    cleanups.push(() => {
      try {
        process.kill(sleeper, 'SIGKILL')
      } catch {
        // Already gone.
      }
    })
    child.kill(9)
    await child.exited

    const faux = fauxCatalog()
    faux.faux.setResponses([fauxAssistantMessage('finished after the restart')])
    const kernel: KernelHandle = await start({
      dir: data.dir,
      faux,
      overrides: { policy: { autonomy: 'all', rules: [] } },
    })
    const client = new Client(kernel.port)
    cleanups.push(async () => {
      client.close()
      await kernel.stop()
    })
    const deadline = Date.now() + 30_000
    let result: Entry | undefined
    while (result === undefined && Date.now() < deadline) {
      const page = await client.call('conversation.entries', {
        id: conversationId,
        query: { order: 'ascending' },
        limit: 200,
      })
      result = (page['items'] as Entry[]).find((entry) => entry.kind === 'pi.tool-result')
      if (result === undefined) await Bun.sleep(25)
    }
    expect(result).toBeDefined()
    // bash is not safe to run twice: the restart ends it as cut off, and the script goes on from there.
    expect(result!.model?.[0]?.details?.calls?.[0]).toMatchObject({ name: 'bash', status: 'error' })
    await client.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === conversationId &&
        note.params['run'] === null,
      30_000,
    )
    const inspect = await kernel.harness.inspect({ signal: new AbortController().signal } as never)
    expect(
      inspect.tasks.filter((task) => task.record.conversationId === (conversationId as never))
        .length,
    ).toBe(0)
  }, 90_000)
})
