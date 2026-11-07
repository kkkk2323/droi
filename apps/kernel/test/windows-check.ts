/**
 * What a cross-compiled Windows kernel does on a real Windows machine, checked with nothing but `bun`.
 *
 * It is deliberately self-contained — no `node_modules`, no imports from `src` — because its whole job is
 * to run beside a `kernel.exe` that was built on another platform. It starts a fake OpenAI provider, hands
 * the kernel a config that points at it, and then asks for the two things that Windows gets wrong:
 *
 *  1. the tool layer: `powershell` must be there, and `bash` must not be, unless Git Bash is installed;
 *  2. the image path: `read` on a PNG must return an image, which is the failure a cross build hides.
 *
 *   bun windows-check.ts <kernel.exe> [--git-bash <dir with Git\bin\bash.exe>]
 *
 * With `--git-bash`, `ProgramFiles` is pointed at that directory for the child, which is how the "Git Bash
 * is installed" case is checked without installing anything into the user's profile.
 */
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { deflateSync } from 'node:zlib'

const executable = process.argv[2]
if (executable === undefined)
  throw new Error('usage: windows-check.ts <kernel.exe> [--git-bash <dir>]')
const gitBashIndex = process.argv.indexOf('--git-bash')
const gitBashRoot = gitBashIndex > 0 ? process.argv[gitBashIndex + 1]! : undefined
const token = 'windows-check-token'

// ── a PNG, written by hand ──────────────────────────────────────────────────────────────────────────
function gradientPng(width: number, height: number): Uint8Array {
  const raw = Buffer.alloc((width * 3 + 1) * height)
  let offset = 0
  for (let y = 0; y < height; y++) {
    raw[offset++] = 0
    for (let x = 0; x < width; x++) {
      raw[offset++] = Math.round((x / width) * 255)
      raw[offset++] = Math.round((y / height) * 255)
      raw[offset++] = 160
    }
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header[8] = 8
  header[9] = 2
  const table = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    table[n] = c >>> 0
  }
  const crc32 = (bytes: Uint8Array): number => {
    let crc = 0xffffffff
    for (const byte of bytes) crc = table[(crc ^ byte) & 0xff]! ^ (crc >>> 8)
    return (crc ^ 0xffffffff) >>> 0
  }
  const chunk = (type: string, data: Buffer): Buffer => {
    const length = Buffer.alloc(4)
    length.writeUInt32BE(data.length, 0)
    const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
    const crc = Buffer.alloc(4)
    crc.writeUInt32BE(crc32(body) >>> 0, 0)
    return Buffer.concat([length, body, crc])
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

// ── the fake provider ───────────────────────────────────────────────────────────────────────────────
type Step = { text: string } | { toolCall: { name: string; arguments: Record<string, unknown> } }
const steps: Step[] = []
const server = Bun.serve({
  port: 0,
  hostname: '127.0.0.1',
  async fetch(request) {
    if (!new URL(request.url).pathname.endsWith('/chat/completions')) {
      return new Response('not found', { status: 404 })
    }
    await request.json()
    const step = steps.shift() ?? { text: 'done' }
    const id = `chatcmpl-${Math.random().toString(36).slice(2, 8)}`
    const chunks: string[] = []
    const send = (delta: unknown, finish: string | null = null) =>
      chunks.push(
        `data: ${JSON.stringify({ id, object: 'chat.completion.chunk', model: 'fake-1', choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`,
      )
    send({ role: 'assistant', content: '' })
    if ('text' in step) {
      send({ content: step.text })
    } else {
      send({
        tool_calls: [
          {
            index: 0,
            id: 'call_1',
            type: 'function',
            function: {
              name: step.toolCall.name,
              arguments: JSON.stringify(step.toolCall.arguments),
            },
          },
        ],
      })
    }
    send({}, 'toolCall' in step ? 'tool_calls' : 'stop')
    chunks.push('data: [DONE]\n\n')
    return new Response(chunks.join(''), { headers: { 'content-type': 'text/event-stream' } })
  },
})

// ── the kernel binary ───────────────────────────────────────────────────────────────────────────────
const work = mkdtempSync(join(tmpdir(), 'droi-kernel-check-'))
const dataDir = join(work, 'data')
const configPath = join(work, 'config.json')
const imagePath = join(work, 'screenshot.png')
writeFileSync(imagePath, gradientPng(2400, 1200))
writeFileSync(
  configPath,
  JSON.stringify(
    {
      providers: [
        {
          id: 'fake',
          baseUrl: `http://127.0.0.1:${server.port}/v1`,
          api: 'openai-completions',
          apiKeyEnv: 'FAKE_PROVIDER_KEY',
          models: [
            {
              id: 'fake-1',
              name: 'Fake',
              contextWindow: 200_000,
              maxTokens: 8000,
              input: ['text', 'image'],
            },
          ],
        },
      ],
      agent: { provider: 'fake', modelId: 'fake-1' },
    },
    null,
    2,
  ),
)

const env: Record<string, string> = {
  ...process.env,
  DROI_KERNEL_TOKEN: token,
  FAKE_PROVIDER_KEY: 'fake-key',
}
if (gitBashRoot !== undefined) env['ProgramFiles'] = gitBashRoot

const child = spawn(executable, ['--data', dataDir, '--port', '0', '--config', configPath], { env })
let log = ''
child.stderr.on('data', (chunk: Buffer) => {
  log += chunk.toString()
})

const handshake = await new Promise<{ port: number }>((resolve, reject) => {
  let buffer = ''
  const timer = setTimeout(() => reject(new Error(`no handshake in 30 s:\n${log}`)), 30_000)
  child.stdout.on('data', (chunk: Buffer) => {
    buffer += chunk.toString()
    const line = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
    if (line === undefined) return
    clearTimeout(timer)
    resolve(JSON.parse(line) as { port: number })
  })
  child.on('exit', (code) => {
    clearTimeout(timer)
    reject(new Error(`the kernel exited ${code}:\n${log}`))
  })
})

class Client {
  #ws: WebSocket
  #id = 1
  #pending = new Map<number, (value: Record<string, unknown>) => void>()
  #waiters: {
    test: (n: { method: string; params: Record<string, unknown> }) => boolean
    resolve: () => void
  }[] = []
  readonly notifications: { method: string; params: Record<string, unknown> }[] = []
  readonly opened: Promise<void>

  constructor(port: number) {
    this.#ws = new WebSocket(`ws://127.0.0.1:${port}/rpc`, {
      headers: { Authorization: `Bearer ${token}` },
    } as never)
    this.opened = new Promise((resolve, reject) => {
      this.#ws.addEventListener('open', () => resolve(), { once: true })
      this.#ws.addEventListener('error', () => reject(new Error('websocket failed')), {
        once: true,
      })
    })
    this.#ws.addEventListener('message', (event) => {
      const frame = JSON.parse(String(event.data)) as Record<string, unknown>
      if (typeof frame['method'] === 'string') {
        const note = {
          method: frame['method'] as string,
          params: (frame['params'] ?? {}) as Record<string, unknown>,
        }
        this.notifications.push(note)
        for (const waiter of [...this.#waiters]) {
          if (!waiter.test(note)) continue
          this.#waiters = this.#waiters.filter((other) => other !== waiter)
          waiter.resolve()
        }
        return
      }
      const settle = this.#pending.get(Number(frame['id']))
      if (settle === undefined) return
      this.#pending.delete(Number(frame['id']))
      if (frame['error'] !== undefined) throw new Error(JSON.stringify(frame['error']))
      settle(frame['result'] as Record<string, unknown>)
    })
  }

  async call(method: string, params?: Record<string, unknown>): Promise<Record<string, unknown>> {
    await this.opened
    const id = this.#id++
    const answer = new Promise<Record<string, unknown>>((resolve) => this.#pending.set(id, resolve))
    this.#ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
    return answer
  }

  waitFor(
    test: (n: { method: string; params: Record<string, unknown> }) => boolean,
    timeoutMs = 60_000,
  ): Promise<void> {
    const seen = this.notifications.find(test)
    if (seen !== undefined) return Promise.resolve()
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('no notification in time')), timeoutMs)
      this.#waiters.push({ test, resolve: () => (clearTimeout(timer), resolve()) })
    })
  }

  close(): void {
    this.#ws.close()
  }
}

type Part = { type: string; text?: string; data?: string; mimeType?: string }
type Entry = { id?: number; kind: string; model?: { content: Part[]; isError?: boolean }[] }

const client = new Client(handshake.port)
const failures: string[] = []
const check = (ok: boolean, message: string): void => {
  process.stdout.write(`${ok ? 'PASS' : 'FAIL'}  ${message}\n`)
  if (!ok) failures.push(message)
}

async function entriesOf(id: number): Promise<Entry[]> {
  const page = await client.call('conversation.entries', {
    id,
    query: { order: 'ascending' },
    limit: 200,
  })
  return page['items'] as Entry[]
}

async function runTurn(input: string, step: Step, id: number): Promise<Entry[]> {
  // Everything an earlier turn wrote stays in the transcript, so a turn is measured by entry IDs.
  const before = await entriesOf(id)
  const seen = new Set(before.map((entry) => entry.id))
  steps.push(step)
  steps.push({ text: 'ok' })
  await client.call('conversation.submit', { id, input })
  const deadline = Date.now() + 120_000
  while (Date.now() < deadline) {
    const entries = await entriesOf(id)
    const fresh = entries.filter((entry) => !seen.has(entry.id))
    const results = fresh.filter((entry) => entry.kind === 'pi.tool-result')
    const answered = fresh.filter((entry) => entry.kind === 'pi.assistant').length >= 2
    if (results.length > 0 && answered) return results
    await Bun.sleep(100)
  }
  throw new Error(`turn "${input}" never finished:\n${log}`)
}

try {
  const info = await client.call('harness.info')
  const extensions = info['extensions'] as { tools: string[] }[]
  const tools = extensions[0]?.tools ?? []
  process.stdout.write(`platform: ${process.platform}\ntools: ${tools.join(', ')}\n`)
  check(
    tools.includes('read') && tools.includes('write') && tools.includes('edit'),
    'the file tools are offered',
  )
  check(tools.includes('powershell'), 'the powershell tool is offered on Windows')
  check(
    gitBashRoot === undefined ? !tools.includes('bash') : tools.includes('bash'),
    gitBashRoot === undefined
      ? 'bash is not offered when Git Bash is missing (it would run WSL)'
      : 'bash is offered when Git Bash is installed',
  )

  const created = await client.call('conversation.create', {
    agent: { cwd: work, model: { provider: 'fake', modelId: 'fake-1' } },
  })
  const id = created['conversationId'] as number

  // 1b: a real command through the platform's own shell, and the paths it reports.
  const shellTool = gitBashRoot === undefined ? 'powershell' : 'bash'
  const command =
    shellTool === 'powershell'
      ? '[System.Environment]::UserName; (Get-Location).Path'
      : 'echo "$USERNAME"; pwd; echo "HOME=$HOME"'
  const results = await runTurn(
    'where am I',
    { toolCall: { name: shellTool, arguments: { command } } },
    id,
  )
  const output = (results[0]?.model?.[0]?.content ?? [])
    .filter((part) => part.type === 'text')
    .map((part) => part.text ?? '')
    .join('')
  process.stdout.write(`--- ${shellTool} output ---\n${output.trim()}\n---\n`)
  check(!output.includes('/mnt/c'), 'the command did not run inside WSL')
  check(!/HOME=\/home\//i.test(output), 'HOME is not a Linux home directory')
  check(/[A-Za-z]:[\\/]/.test(output), 'the working directory is a Windows path')

  // 1c: the image path, in a cross-compiled product.
  const imageResults = await runTurn(
    'see the screenshot',
    { toolCall: { name: 'read', arguments: { path: imagePath } } },
    id,
  )
  const imageContent = imageResults[0]?.model?.[0]?.content ?? []
  const image = imageContent.find((part) => part.type === 'image')
  void imageContent
  if (image?.data === undefined) {
    const text = imageContent
      .filter((part) => part.type === 'text')
      .map((part) => part.text ?? '')
      .join('')
    process.stdout.write(`--- read output ---\n${text.slice(0, 1500)}\n---\n`)
  }
  check(image?.data !== undefined, 'read returned an image')
  if (image?.data !== undefined) {
    const bytes = Buffer.from(image.data, 'base64')
    check(
      bytes.subarray(1, 4).toString('ascii') === 'PNG',
      `the image is a PNG (${bytes.length} bytes)`,
    )
  }
  // Windows has no SIGTERM a supervisor can send, so `kernel.shutdown` is the only clean stop: it must
  // answer, close the storage, and exit 0.
  const exited = new Promise<number | null>((resolve) => {
    if (child.exitCode !== null) return resolve(child.exitCode)
    child.on('exit', (code) => resolve(code))
  })
  const answer = await client.call('kernel.shutdown')
  check(answer['status'] === 'closing', 'kernel.shutdown answered before closing')
  const code = await Promise.race([exited, Bun.sleep(20_000).then(() => 'timeout' as const)])
  check(code === 0, `the kernel exited 0 after shutdown (got ${String(code)})`)
} finally {
  client.close()
  server.stop(true)
  child.kill()
  // Windows releases the SQLite file, and everything else, only once the process is really gone.
  const exited = new Promise<void>((resolve) => {
    if (child.exitCode !== null) return resolve()
    child.on('exit', () => resolve())
  })
  await Promise.race([exited, Bun.sleep(10_000)])
  try {
    rmSync(work, { recursive: true, force: true, maxRetries: 40, retryDelay: 250 })
  } catch {
    // A temp directory the OS still holds is not a result; it is in %TEMP%, which the user cleans.
  }
}

if (failures.length > 0) {
  process.stderr.write(`\n${failures.length} check(s) failed\n`)
  process.exit(1)
}
process.stdout.write('\nall checks passed\n')
