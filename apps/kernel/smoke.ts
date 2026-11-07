/**
 * What a *built* kernel does, checked against the real binary: start it, drive it through a local fake
 * provider, and call `read` on a PNG. The tool result must carry an image.
 *
 * This is the check a native build cannot make for you. `bun build --compile` into another platform
 * keeps the Photon WebAssembly bytes but used to lose the way to them, so images failed only in the
 * cross-compiled product — a Windows installer built on Ubuntu, with every macOS test green. So CI runs
 * this against a cross-compiled binary, and the assertion is an image, not the presence of a file.
 *
 *   bun run smoke.ts <executable> [--data <dir>] [--keep]
 */
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { Client, TOKEN } from './test/helpers.ts'
import { fakeKernelConfig, startFakeProvider } from './test/fake-provider.ts'
import { gradientPng } from './test/png.ts'

const executable = process.argv[2]
if (executable === undefined) throw new Error('usage: smoke.ts <executable> [--data <dir>]')
const dataIndex = process.argv.indexOf('--data')
const keep = process.argv.includes('--keep')
const dataDir =
  dataIndex > 0 ? process.argv[dataIndex + 1]! : mkdtempSync(join(tmpdir(), 'kernel-smoke-'))
const workDir = mkdtempSync(join(tmpdir(), 'kernel-smoke-work-'))

type Handshake = { port: number; pid: number; version: string }

async function startBinary(
  executablePath: string,
  dir: string,
  configPath: string,
): Promise<{ child: ReturnType<typeof spawn>; handshake: Handshake; stderr: () => string }> {
  const child = spawn(executablePath, ['--data', dir, '--port', '0', '--config', configPath], {
    env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN, FAKE_PROVIDER_KEY: 'fake-key' },
  })
  let log = ''
  child.stderr.on('data', (chunk: Buffer) => {
    log += chunk.toString()
  })
  const handshake = await new Promise<Handshake>((resolve, reject) => {
    let buffer = ''
    const timer = setTimeout(() => reject(new Error(`no handshake in 30 s: ${log}`)), 30_000)
    child.stdout.on('data', (chunk: Buffer) => {
      buffer += chunk.toString()
      const line = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
      if (line === undefined) return
      clearTimeout(timer)
      resolve(JSON.parse(line) as Handshake)
    })
    child.on('exit', (code) => {
      clearTimeout(timer)
      reject(new Error(`the kernel exited ${code}: ${log}`))
    })
  })
  return { child, handshake, stderr: () => log }
}

async function main(): Promise<void> {
  const provider = startFakeProvider()
  const configPath = join(workDir, 'config.json')
  writeFileSync(configPath, JSON.stringify(fakeKernelConfig(provider.baseUrl), null, 2))
  const imagePath = join(workDir, 'screenshot.png')
  writeFileSync(imagePath, gradientPng(2400, 1200))

  const binary = await startBinary(executable, dataDir, configPath)
  const client = new Client(binary.handshake.port)
  try {
    const info = await client.call('harness.info')
    process.stdout.write(`binary: ${String(info['version'])}\n`)

    provider.push({ toolCall: { name: 'read', arguments: { path: imagePath } } })
    provider.push({ text: 'that is a gradient' })
    const created = await client.call('conversation.create', {
      agent: { cwd: workDir, model: { provider: 'fake', modelId: 'fake-1' } },
    })
    const id = created['conversationId'] as number
    await client.call('conversation.submit', { id, input: 'read the screenshot' })

    const deadline = Date.now() + 120_000
    for (;;) {
      if (Date.now() > deadline) throw new Error(`no image came back:\n${binary.stderr()}`)
      const page = await client.call('conversation.entries', {
        id,
        query: { order: 'ascending' },
        limit: 100,
      })
      for (const entry of page['items'] as {
        kind: string
        model?: { content: { type: string; data?: string; mimeType?: string; text?: string }[] }[]
      }[]) {
        if (entry.kind !== 'pi.tool-result') continue
        const content = entry.model?.[0]?.content ?? []
        const image = content.find((part) => part.type === 'image')
        if (image?.data === undefined) {
          const text = content.find((part) => part.type === 'text')?.text ?? ''
          throw new Error(`the read tool returned no image: ${text}\n${binary.stderr()}`)
        }
        const bytes = Buffer.from(image.data, 'base64')
        if (bytes.subarray(1, 4).toString('ascii') !== 'PNG')
          throw new Error('the image is not a PNG')
        process.stdout.write(`image: ${image.mimeType ?? '?'}, ${bytes.length} bytes\n`)
        return
      }
      await Bun.sleep(50)
    }
  } finally {
    client.close()
    binary.child.kill('SIGTERM')
    provider.stop()
    if (!keep) {
      rmSync(dataDir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
      rmSync(workDir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
    }
  }
}

if (import.meta.main) await main()
