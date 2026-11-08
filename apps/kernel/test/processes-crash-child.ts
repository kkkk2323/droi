/**
 * A kernel that starts one background process and then gets killed on purpose. Started by
 * `processes.test.ts`; it prints where it listens and which process it started.
 */
import {
  createModels,
  fauxAssistantMessage,
  fauxProvider,
  fauxToolCall,
} from '@earendil-works/pi-ai'
import { config, Client, TOKEN } from './helpers.ts'
import { startKernel } from '../src/kernel.ts'

const dir = process.argv[2]
const command = process.argv[3]
if (dir === undefined || command === undefined) {
  throw new Error('usage: processes-crash-child.ts <data dir> <command>')
}

const faux = fauxProvider()
faux.setResponses([
  fauxAssistantMessage(fauxToolCall('bash_background', { command }), { stopReason: 'toolUse' }),
  fauxAssistantMessage('started'),
])
const models = createModels()
models.setProvider(faux.provider)
const model = faux.getModel()

const kernel = await startKernel(config(dir), { token: TOKEN, models, noLock: true })
const client = new Client(kernel.port)
const created = await client.call('conversation.create', {
  agent: { model: { provider: model.provider, modelId: model.id }, cwd: process.cwd() },
})
const conversationId = created['conversationId'] as number
await client.call('conversation.submit', { id: conversationId, input: 'start it' })
const running = await client.waitFor(
  (note) => note.method === 'process.changed' && note.params['status'] === 'running',
)
const listed = await client.call('process.list', { id: conversationId })
const processes = listed['processes'] as { id: string; pid: number }[]
const started = processes.find((p) => p.id === running.params['processId'])!
process.stdout.write(
  `${JSON.stringify({ port: kernel.port, conversationId, processId: started.id, pid: started.pid })}\n`,
)
await new Promise<never>(() => {})
