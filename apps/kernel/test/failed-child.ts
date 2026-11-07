/**
 * A kernel whose storage breaks under it, to check what the Host sees.
 *
 * Started by `review.test.ts`. It hands the kernel a storage whose next commit throws, which is what a
 * platform reset looks like to Pi: the Session fails for good. The kernel must answer `-32010` from then
 * on and exit non-zero, because a Host that is not told to replace it keeps a process that cannot write.
 */
import { createModels, fauxProvider } from '@earendil-works/pi-ai'
import { config, Client, TOKEN } from './helpers.ts'
import { startKernel } from '../src/kernel.ts'

const dir = process.argv[2]
if (dir === undefined) throw new Error('usage: failed-child.ts <data dir>')

const faux = fauxProvider()
faux.setResponses([])
const models = createModels()
models.setProvider(faux.provider)

const kernel = await startKernel(config(dir), { token: TOKEN, models, noLock: true })
const client = new Client(kernel.port)
await client.opened
process.stdout.write(`${JSON.stringify({ port: kernel.port })}\n`)

// Break the file underneath the kernel: the next commit cannot be stored.
const { Database } = await import('bun:sqlite')
setTimeout(() => {
  try {
    const outside = new Database(`${dir}/kernel.sqlite`)
    outside.exec('PRAGMA journal_mode = WAL')
    outside.exec('BEGIN EXCLUSIVE')
    // Hold the lock; every write the kernel attempts times out and fails.
    setTimeout(() => outside.close(), 30_000)
  } catch {
    // An error here is the test's own; the kernel's behaviour is what matters.
  }
}, 200)
// A request that must commit, to trigger the failure.
setTimeout(() => {
  void client.call('conversation.create', {}).catch(() => undefined)
}, 600)
await new Promise<never>(() => {})
