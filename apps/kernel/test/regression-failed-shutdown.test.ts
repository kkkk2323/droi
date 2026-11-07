import { test, expect } from 'bun:test'
import { spawn } from 'node:child_process'
import { Client, tempData } from './helpers.ts'

test('R3 shutdown cannot override storage-failed code 3 with clean exit 0', async () => {
  const data = tempData()
  const child = spawn(process.execPath, [
    new URL('./failed-child.ts', import.meta.url).pathname,
    data.dir,
  ])
  const exited = new Promise<number | null>((resolve) => child.on('exit', resolve))
  let log = ''
  child.stderr.on('data', (chunk) => (log += chunk))
  let client: Client | undefined
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
    await client.waitFor((n) => n.method === 'warning' && n.params.kind === 'kernel_failed', 15000)
    const reply = await client.call('kernel.shutdown').catch((error) => ({ code: error.code }))
    const status = await exited
    console.log('R3_FAILED_SHUTDOWN', { reply, exit: status })
    expect(status).toBe(3)
    expect(reply.code).toBe(-32010)
  } finally {
    client?.close()
    child.kill('SIGKILL')
    data.cleanup()
  }
}, 25000)
