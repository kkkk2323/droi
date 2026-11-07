/**
 * A kernel that gets killed on purpose. Started by `crash.test.ts` through `bun run`; it opens the data
 * directory it is given, starts a run the model never finishes, and prints where it listens.
 *
 * It is a real kernel process — the same `main.ts` path, minus the lock — so the test kills a real
 * SQLite writer mid-run and restarts over what it left.
 */
import { createModels, fauxProvider } from '@earendil-works/pi-ai'
import { config } from './helpers.ts'
import { startKernel } from '../src/kernel.ts'
import { Client, TOKEN } from './helpers.ts'

const dir = process.argv[2]
if (dir === undefined) throw new Error('usage: crash-child.ts <data dir>')

const faux = fauxProvider()
// The run never gets its answer: this is the model call the crash interrupts.
faux.setResponses([async () => new Promise<never>(() => {})])
const models = createModels()
models.setProvider(faux.provider)
const model = faux.getModel()

const kernel = await startKernel(config(dir), {
  token: TOKEN,
  models,
  noLock: true,
})
const client = new Client(kernel.port)
const created = await client.call('conversation.create', {
  agent: { model: { provider: model.provider, modelId: model.id }, cwd: process.cwd() },
})
const conversationId = created['conversationId'] as number
await client.call('conversation.submit', { id: conversationId, input: 'survive the crash' })
// Wait until the run is in flight, so the kill lands in the middle of it.
const deadline = Date.now() + 10_000
while (Date.now() < deadline) {
  const busy = client.notifications.find(
    (note) => note.method === 'conversation.busy' && note.params['id'] === conversationId,
  )
  if (busy?.params['run'] != null) break
  await Bun.sleep(10)
}
process.stdout.write(`${JSON.stringify({ port: kernel.port, conversationId })}\n`)
// Alive until killed.
await new Promise<never>(() => {})
