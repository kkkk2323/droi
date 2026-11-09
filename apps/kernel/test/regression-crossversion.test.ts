import { test, expect } from 'bun:test'
import { spawn } from 'node:child_process'
import { Client, TOKEN, fauxCatalog, start, tempData } from './helpers.ts'

test.skipIf(!process.env.OLD_KERNEL_ROOT)(
  'R3 unchanged create retry survives previous-head fingerprint format',
  async () => {
    const data = tempData()
    const child = spawn(
      process.execPath,
      [`${process.env.OLD_KERNEL_ROOT}/src/main.ts`, '--data', data.dir],
      {
        env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN },
      },
    )
    const exited = new Promise((resolve) => child.on('exit', resolve))
    let log = ''
    child.stderr.on('data', (chunk) => (log += chunk))
    let client: Client | undefined
    let kernel: Awaited<ReturnType<typeof start>> | undefined
    try {
      const port = await new Promise<number>((resolve, reject) => {
        let out = ''
        const timer = setTimeout(() => reject(new Error(log)), 10000)
        child.stdout.on('data', (chunk) => {
          out += chunk
          if (out.includes('\n')) {
            clearTimeout(timer)
            resolve(JSON.parse(out.split('\n')[0]!).port)
          }
        })
      })
      client = new Client(port)
      const params = { requestId: 'same-request-across-upgrade', kind: 'chat', cwd: '/tmp' }
      const first = await client.call('conversation.create', params)
      client.close()
      child.kill('SIGTERM')
      await exited
      kernel = await start({ dir: data.dir, faux: fauxCatalog() })
      client = new Client(kernel.port)
      const second = (await client
        .call('conversation.create', params)
        .catch((error: { code: number }) => ({
          conversationId: undefined,
          code: error.code,
        }))) as { conversationId?: number; code?: number }
      expect(second.conversationId).toBe(first['conversationId'] as number)
    } finally {
      child.kill('SIGKILL')
      client?.close()
      await kernel?.stop()
      data.cleanup()
    }
  },
  20000,
)
