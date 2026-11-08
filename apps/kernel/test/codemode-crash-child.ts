/**
 * A kernel killed in the middle of a codemode script. Started by `codemode-crash.test.ts`: the model runs a script
 * whose `bash` call writes its pid and then sleeps, and this prints where it listens once that call is running.
 */
import {
  createModels,
  fauxAssistantMessage,
  fauxProvider,
  fauxToolCall,
} from '@earendil-works/pi-ai'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { config } from './helpers.ts'
import { startKernel } from '../src/kernel.ts'
import { Client, TOKEN } from './helpers.ts'

const dir = process.argv[2]
const work = process.argv[3]
if (dir === undefined || work === undefined) {
  throw new Error('usage: codemode-crash-child.ts <data dir> <work dir>')
}
const pidFile = join(work, 'sleep.pid')

const faux = fauxProvider()
faux.setResponses([
  fauxAssistantMessage(
    fauxToolCall('codemode', {
      code: `const r = await tools.bash({ command: "echo $$ > ${pidFile} && sleep 30" }); return "after"`,
    }),
    { stopReason: 'toolUse' },
  ),
])
const models = createModels()
models.setProvider(faux.provider)
const model = faux.getModel()

const kernel = await startKernel(config(dir, { policy: { autonomy: 'all', rules: [] } }), {
  token: TOKEN,
  models,
  noLock: true,
})
const client = new Client(kernel.port)
const created = await client.call('conversation.create', {
  agent: { model: { provider: model.provider, modelId: model.id }, cwd: work },
})
const conversationId = created['conversationId'] as number
await client.call('conversation.submit', { id: conversationId, input: 'run a long script' })
const deadline = Date.now() + 20_000
while (Date.now() < deadline && !existsSync(pidFile)) await Bun.sleep(10)
process.stdout.write(`${JSON.stringify({ port: kernel.port, conversationId })}\n`)
await new Promise<never>(() => {})
