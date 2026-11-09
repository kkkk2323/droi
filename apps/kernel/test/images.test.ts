import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import { gradientPng } from './png.ts'
import type { KernelHandle } from '../src/kernel.ts'

let data: ReturnType<typeof tempData>
let faux: Faux
let kernel: KernelHandle

beforeEach(async () => {
  data = tempData()
  faux = fauxCatalog()
  kernel = await start({ dir: data.dir, faux })
})

afterEach(async () => {
  await kernel.stop()
  data.cleanup()
})

describe('the image path', () => {
  test('read returns ImageContent for a PNG, through Photon and the shipped wasm', async () => {
    const path = join(data.dir, 'gradient.png')
    writeFileSync(path, gradientPng(2400, 1200))
    const client = new Client(kernel.port)
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: data.dir },
    })
    const id = created['conversationId'] as number

    faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('read', { path }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('that is a gradient'),
    ])
    await client.call('conversation.submit', { id, input: 'look at this screenshot' })

    const result = await waitForToolResult(client, id, 0, 60_000)
    expect(result.isError).toBe(false)
    const images = result.content.filter((part) => part.type === 'image')
    expect(images.length).toBe(1)
    const image = images[0] as { type: 'image'; data: string; mimeType: string }
    expect(image.mimeType).toBe('image/png')
    // A real encode, not a copy of the file: the processor resized it for the model.
    const bytes = Buffer.from(image.data, 'base64')
    expect(bytes.length).toBeGreaterThan(1000)
    expect(bytes.subarray(1, 4).toString('ascii')).toBe('PNG')
    client.close()
  }, 120_000)
})

type ToolContentPart = { type: string; text?: string; data?: string; mimeType?: string }
type ToolResult = { isError: boolean; content: ToolContentPart[] }

async function waitForToolResult(
  client: Client,
  id: number,
  index: number,
  timeoutMs: number,
): Promise<ToolResult> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const entries = page['items'] as {
      kind: string
      model?: { role: string; content: ToolContentPart[]; isError?: boolean }[]
    }[]
    const results = entries.filter((entry) => entry.kind === 'pi.tool-result')
    const found = results[index]
    const message = found?.model?.[0]
    if (message !== undefined) {
      return { isError: message.isError ?? false, content: message.content }
    }
    await Bun.sleep(25)
  }
  throw new Error(`no tool result ${index} in ${timeoutMs} ms`)
}
