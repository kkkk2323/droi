/**
 * What the kernel costs, measured the way `shared/perf/` measures Pi: a process tree sampled from `ps`,
 * a local fake model, medians over several runs.
 *
 *   bun run bench.ts [--runs 3] [--out <dir>] [--executable <path>]
 *
 * Scenes, chosen to match `shared/perf/pi-perf-report.md` where the two are comparable:
 *
 *  - **A, cold start**: `spawn` to the handshake line, then idle RSS after half a second.
 *  - **B, sessions**: idle RSS holding 1, then 5, then 10 conversations that each did one turn. The
 *    marginal cost of a conversation is what Pi's shape buys; it is the number the report leads with.
 *  - **C, 100 rounds** of `read` and of `bash` in one conversation, timed and sampled at the peak: the
 *    same number `pi-perf-report.md` uses.
 */
import { spawn, execFile } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { Client, TOKEN } from './test/helpers.ts'
import { startFakeProvider, type FakeProvider, type Step } from './test/fake-provider.ts'

type Args = { runs: number; out: string; executable: string | undefined }

function parse(argv: string[]): Args {
  const take = (flag: string, fallback: string): string => {
    const index = argv.indexOf(flag)
    return index >= 0 && argv[index + 1] !== undefined ? argv[index + 1]! : fallback
  }
  return {
    runs: Number(take('--runs', '3')),
    out: take('--out', join(tmpdir(), 'kernel-bench')),
    executable: argv.includes('--executable') ? take('--executable', '') : undefined,
  }
}

const args = parse(process.argv.slice(2))
mkdirSync(args.out, { recursive: true })

/** RSS of one process and its descendants, in MB: the kernel is one process, but a bash call is a child. */
function treeRssMb(rootPid: number): Promise<number> {
  return new Promise((resolve) => {
    execFile('ps', ['-Ao', 'pid=,ppid=,rss='], { maxBuffer: 64 * 1024 * 1024 }, (error, stdout) => {
      if (error) return resolve(0)
      const rows: { pid: number; ppid: number; rss: number }[] = []
      for (const line of stdout.split('\n')) {
        const match = line.trim().match(/^(\d+)\s+(\d+)\s+(\d+)$/)
        if (match)
          rows.push({ pid: Number(match[1]), ppid: Number(match[2]), rss: Number(match[3]) })
      }
      const children = new Map<number, number[]>()
      const rss = new Map<number, number>()
      for (const row of rows) {
        rss.set(row.pid, row.rss)
        const list = children.get(row.ppid) ?? []
        list.push(row.pid)
        children.set(row.ppid, list)
      }
      let kb = 0
      const seen = new Set<number>()
      const stack = [rootPid]
      while (stack.length > 0) {
        const pid = stack.pop()!
        if (seen.has(pid)) continue
        seen.add(pid)
        kb += rss.get(pid) ?? 0
        for (const child of children.get(pid) ?? []) stack.push(child)
      }
      resolve(Math.round((kb / 1024) * 10) / 10)
    })
  })
}

type Running = {
  client: Client
  rss: () => Promise<number>
  stop: () => Promise<void>
}

/**
 * Start the kernel under measurement. Without `--executable` it runs `src/main.ts` under Bun — the same
 * code the binary holds — and with one it measures the product a user gets.
 */
async function startKernelProcess(provider: FakeProvider, dataDir: string): Promise<Running> {
  const work = mkdtempSync(join(tmpdir(), 'kernel-bench-work-'))
  const configPath = join(work, 'config.json')
  // The env override is the same mechanism a test uses; nothing here needs a config file to be read.
  const providers = [
    {
      id: 'fake',
      baseUrl: provider.baseUrl,
      api: 'openai-completions',
      apiKeyEnv: 'FAKE_PROVIDER_KEY',
      models: [
        {
          id: 'fake-1',
          name: 'Fake',
          contextWindow: 200_000,
          maxTokens: 8_000,
          input: ['text', 'image'],
        },
      ],
    },
  ]
  writeFileSync(configPath, JSON.stringify({ providers }, null, 2))
  const command =
    args.executable === undefined
      ? [process.execPath, new URL('./src/main.ts', import.meta.url).pathname]
      : [args.executable]
  const child = spawn(command[0]!, [...command.slice(1), '--data', dataDir, '--port', '0'], {
    env: {
      ...process.env,
      DROI_KERNEL_TOKEN: TOKEN,
      FAKE_PROVIDER_KEY: 'fake-key',
      DROI_KERNEL_PROVIDERS: JSON.stringify(providers),
    },
  })
  let log = ''
  child.stderr.on('data', (chunk: Buffer) => {
    log += chunk.toString()
  })
  const handshake = await new Promise<{ port: number }>((resolve, reject) => {
    let buffer = ''
    const timer = setTimeout(() => reject(new Error(`no handshake in 30 s: ${log}`)), 30_000)
    child.stdout.on('data', (chunk: Buffer) => {
      buffer += chunk.toString()
      const line = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
      if (line === undefined) return
      clearTimeout(timer)
      resolve(JSON.parse(line) as { port: number })
    })
    child.on('exit', (code) => {
      clearTimeout(timer)
      reject(new Error(`the kernel exited ${code}: ${log}`))
    })
  })
  const client = new Client(handshake.port)
  const exited = new Promise<void>((resolve) => {
    if (child.exitCode !== null) resolve()
    else child.on('exit', () => resolve())
  })
  return {
    client,
    rss: () => treeRssMb(child.pid!),
    stop: async () => {
      try {
        await client.call('kernel.shutdown')
      } catch {
        child.kill('SIGKILL')
      }
      await Promise.race([exited, Bun.sleep(15_000)])
      client.close()
      try {
        rmSync(work, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 })
      } catch {
        // A temp dir the OS still holds is not a measurement.
      }
    },
  }
}

const median = (values: number[]): number => {
  const sorted = [...values].sort((a, b) => a - b)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 1 ? sorted[middle]! : (sorted[middle - 1]! + sorted[middle]!) / 2
}

async function temporary<T>(fn: (dir: string) => Promise<T>): Promise<T> {
  const dir = mkdtempSync(join(tmpdir(), 'kernel-bench-data-'))
  try {
    return await fn(dir)
  } finally {
    try {
      rmSync(dir, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 })
    } catch {
      // Windows and a still-closing SQLite file: a temp directory left behind is not a result.
    }
  }
}

/** One turn: submit, then wait until its own entries appear. */
/**
 * One turn, waited for the way a client waits: subscribe to `conversation.busy` and stop when the run
 * ends. The first version of this asked `conversation.entries` in a loop, which measured the client's
 * page fetches as much as the kernel — a hundred rounds of that is a thousand extra reads.
 */
async function turn(
  running: Running,
  provider: FakeProvider,
  id: number,
  input: string,
  step: Step,
): Promise<void> {
  const before = running.client.notifications.length
  provider.set('toolCall' in step ? [step, { text: 'ok' }] : [step])
  await running.client.call('conversation.submit', { id, input })
  const deadline = Date.now() + 60_000
  let started = false
  while (Date.now() < deadline) {
    for (const note of running.client.notifications.slice(before)) {
      if (note.method !== 'conversation.busy' || note.params['id'] !== id) continue
      if (note.params['run'] !== null) started = true
      else if (started) return
    }
    await Bun.sleep(5)
  }
  throw new Error(`the turn "${input}" never finished`)
}

async function sceneA(provider: FakeProvider): Promise<{ readyMs: number; idleMb: number }> {
  return temporary(async (dir) => {
    const started = performance.now()
    const running = await startKernelProcess(provider, dir)
    const readyMs = performance.now() - started
    await Bun.sleep(500)
    const idleMb = await running.rss()
    await running.stop()
    return { readyMs: Math.round(readyMs), idleMb }
  })
}

async function sceneB(provider: FakeProvider, sessions: number): Promise<number> {
  return temporary(async (dir) => {
    const running = await startKernelProcess(provider, dir)
    const ids: number[] = []
    for (let index = 0; index < sessions; index++) {
      const created = await running.client.call('conversation.create', {
        agent: { cwd: process.cwd(), model: { provider: 'fake', modelId: 'fake-1' } },
      })
      ids.push(created['conversationId'] as number)
    }
    for (const id of ids) await turn(running, provider, id, `hello ${id}`, { text: 'hi there' })
    await Bun.sleep(1000)
    const mb = await running.rss()
    await running.stop()
    return mb
  })
}

async function sceneRounds(
  provider: FakeProvider,
  tool: 'read' | 'bash',
  rounds: number,
): Promise<{ seconds: number; peakMb: number }> {
  return temporary(async (dir) => {
    const filePath = join(dir, 'sample.txt')
    writeFileSync(
      filePath,
      Array.from({ length: 400 }, (_, line) => `line ${line} of the sample file`).join('\n'),
    )
    const running = await startKernelProcess(provider, dir)
    const created = await running.client.call('conversation.create', {
      agent: { cwd: dir, model: { provider: 'fake', modelId: 'fake-1' } },
    })
    const id = created['conversationId'] as number
    let peakMb = 0
    const stopSampling = { done: false }
    const sampler = (async () => {
      while (!stopSampling.done) {
        peakMb = Math.max(peakMb, await running.rss())
        await Bun.sleep(100)
      }
    })()
    const started = performance.now()
    for (let round = 0; round < rounds; round++) {
      const step: Step =
        tool === 'read'
          ? {
              toolCall: {
                name: 'read',
                arguments: { path: filePath, offset: (round % 20) + 1, limit: 5 },
              },
            }
          : { toolCall: { name: 'bash', arguments: { command: `echo round-${round}` } } }
      await turn(running, provider, id, `round ${round}`, step)
    }
    const seconds = (performance.now() - started) / 1000
    stopSampling.done = true
    await sampler
    await running.stop()
    return { seconds: Math.round(seconds * 100) / 100, peakMb }
  })
}

async function main(): Promise<void> {
  const provider = startFakeProvider()
  const rows: { scene: string; value: string }[] = []
  const raw: Record<string, unknown> = {
    platform: process.platform,
    arch: process.arch,
    runs: args.runs,
    executable: args.executable === undefined ? 'src/main.ts under Bun' : args.executable,
    measuredAt: new Date().toISOString(),
  }

  const cold: { readyMs: number; idleMb: number }[] = []
  for (let run = 0; run < args.runs; run++) cold.push(await sceneA(provider))
  const ready = median(cold.map((value) => value.readyMs))
  const idle = median(cold.map((value) => value.idleMb))
  rows.push({ scene: 'A: spawn to handshake', value: `${ready.toFixed(0)} ms` })
  rows.push({ scene: 'A: idle RSS', value: `${idle.toFixed(1)} MB` })
  raw['cold'] = { readyMs: ready, idleMb: idle }
  process.stdout.write(`A: ${ready.toFixed(0)} ms, idle ${idle.toFixed(1)} MB\n`)

  const held: Record<string, number> = {}
  for (const sessions of [1, 5, 10]) {
    const samples: number[] = []
    for (let run = 0; run < args.runs; run++) samples.push(await sceneB(provider, sessions))
    const mb = median(samples)
    held[String(sessions)] = mb
    rows.push({
      scene: `B: RSS holding ${sessions} conversation${sessions === 1 ? '' : 's'}`,
      value: `${mb.toFixed(1)} MB`,
    })
    process.stdout.write(`B${sessions}: ${mb.toFixed(1)} MB\n`)
  }
  const marginal = (held['10']! - held['1']!) / 9
  rows.push({ scene: 'B: marginal per conversation', value: `${marginal.toFixed(1)} MB` })
  raw['sessions'] = { ...held, marginalMb: Math.round(marginal * 10) / 10 }

  for (const tool of ['read', 'bash'] as const) {
    const samples: { seconds: number; peakMb: number }[] = []
    for (let run = 0; run < args.runs; run++) samples.push(await sceneRounds(provider, tool, 100))
    const seconds = median(samples.map((value) => value.seconds))
    const peak = median(samples.map((value) => value.peakMb))
    rows.push({ scene: `C: 100 ${tool} rounds`, value: `${seconds.toFixed(2)} s` })
    rows.push({ scene: `C: peak RSS during 100 ${tool} rounds`, value: `${peak.toFixed(1)} MB` })
    raw[tool] = { seconds, peakMb: peak }
    process.stdout.write(`C-${tool}: ${seconds.toFixed(2)} s, peak ${peak.toFixed(1)} MB\n`)
  }

  provider.stop()
  const lines = [
    "# The kernel's own numbers",
    '',
    `${raw['measuredAt'] as string} · ${process.platform} ${process.arch} · ${args.runs} runs, medians · ${
      raw['executable'] as string
    }`,
    '',
    '| scene | median |',
    '| --- | ---: |',
    ...rows.map((row) => `| ${row.scene} | ${row.value} |`),
    '',
    'Measured the way `shared/perf/` measures Pi: a local fake model over HTTP, one process tree sampled',
    'from `ps` every 100 ms, and the medians reported. Compare with `shared/perf/pi-perf-report.md` — the',
    "fake model answers instantly, so these are the framework's own costs, not end-to-end latency.",
    '',
  ]
  writeFileSync(join(args.out, 'kernel-bench.md'), lines.join('\n'))
  writeFileSync(join(args.out, 'kernel-bench.json'), JSON.stringify(raw, null, 2))
  process.stdout.write('\n' + lines.join('\n'))
  // The fake provider holds a socket; nothing else keeps the process alive.
  process.exit(0)
}

await main()
