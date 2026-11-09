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

/**
 * Stop the binary and wait until it is really gone.
 *
 * The wait is what makes the cleanup below work on Windows: a kernel holds `kernel.lock.db` open for its
 * whole life, so removing the data directory before the process has exited fails with `EBUSY` — which is
 * exactly how the cross-compiled smoke failed in CI, once the lock became a real file the kernel keeps
 * open. On Windows `kill('SIGTERM')` terminates the process asynchronously, so the removal raced it.
 */
async function stop(child: ReturnType<typeof spawn>): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()))
  child.kill('SIGTERM')
  const timer = setTimeout(() => child.kill('SIGKILL'), 10_000)
  timer.unref?.()
  await exited
  clearTimeout(timer)
}

const provider = startFakeProvider()

async function main(): Promise<void> {
  const configPath = join(workDir, 'config.json')
  writeFileSync(configPath, JSON.stringify(fakeKernelConfig(provider.baseUrl), null, 2))
  const imagePath = join(workDir, 'screenshot.png')
  writeFileSync(imagePath, gradientPng(2400, 1200))

  const binary = await startBinary(executable, dataDir, configPath)
  const client = new Client(binary.handshake.port)
  try {
    const info = await client.call('harness.info')
    process.stdout.write(`binary: ${String(info['version'])}\n`)
    // The Host's first policy.set: until one comes, a tool above "ask" waits for a person.
    await client.call('policy.set', { policy: { autonomy: 'all' } })

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
        await codemode(client, binary.stderr)
        return
      }
      await Bun.sleep(50)
    }
  } finally {
    client.close()
    // Before removing the data directory: the kernel holds its lock database open until it exits.
    await stop(binary.child)
    provider.stop()
    if (!keep) {
      rmSync(dataDir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
      rmSync(workDir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
    }
  }
}

/**
 * A codemode script, which runs in QuickJS in a worker thread: both the wasm and the worker are files a single-file
 * binary has to carry itself (src/codemode/sandbox.ts), so only a built binary shows they are there.
 */
async function codemode(client: Client, stderr: () => string): Promise<void> {
  provider.push({
    toolCall: { name: 'codemode', arguments: { code: 'text(String(6 * 7)); return 1' } },
  })
  provider.push({ text: 'the script ran' })
  const created = await client.call('conversation.create', {
    agent: { cwd: workDir, model: { provider: 'fake', modelId: 'fake-1' } },
  })
  const id = created['conversationId'] as number
  await client.call('conversation.submit', { id, input: 'run a script' })
  const deadline = Date.now() + 60_000
  for (;;) {
    if (Date.now() > deadline) throw new Error(`the script never answered:\n${stderr()}`)
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 100,
    })
    const result = (
      page['items'] as {
        kind: string
        model?: { content: { text?: string }[]; isError?: boolean }[]
      }[]
    ).find((entry) => entry.kind === 'pi.tool-result')
    if (result !== undefined) {
      const text = (result.model?.[0]?.content ?? []).map((part) => part.text ?? '').join('\n')
      if (result.model?.[0]?.isError === true || !text.includes('42')) {
        throw new Error(`the script failed: ${text}\n${stderr()}`)
      }
      process.stdout.write('codemode: a script ran\n')
      return
    }
    await Bun.sleep(50)
  }
}
if (import.meta.main) await main()
