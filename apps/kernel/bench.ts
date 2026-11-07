/**
 * What the kernel costs, measured the way `shared/perf/` measures Pi — a process-tree sampler and a
 * local fake model — so the numbers compare with `shared/perf/pi-perf-report.md` directly.
 *
 *   bun run bench.ts [--runs 3] [--out /tmp/kernel-bench] [--executable <path>]
 *
 * Three scenes:
 *
 *  - **A, cold start**: from `spawn` to the handshake line, then idle RSS.
 *  - **B, sessions**: idle RSS holding 1, then 5, then 10 conversations that each did one turn. That
 *    is where Pi's shape shows: one process, about 5 MB a conversation.
 *  - **C, 100 read rounds** and **D, 100 bash rounds** in one conversation, timed and sampled.
 *
 * Each scene runs `--runs` times and reports the median, which is what `pi-perf-report.md` quotes.
 */
import { spawn, execFile } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { Client, TOKEN } from './test/helpers.ts'
import { startFakeProvider, type FakeProvider, type Step } from './test/fake-provider.ts'
import { gradientPng } from './test/png.ts'

type Args = { runs: number; out: string; executable: string | undefined }

function parse(argv: string[]): Args {
  const take = (flag: string, fallback: string): string => {
    const index = argv.indexOf(flag)
    return index >= 0 && argv[index + 1] !== undefined ? argv[index + 1]! : fallback
  }
  const runs = Number(take('--runs', '3'))
  const executable = argv.includes('--executable') ? take('--executable', '') : undefined
  return { runs, out: take('--out', join(tmpdir(), 'kernel-bench')), executable }
}

const args = parse(process.argv.slice(2))
mkdirSync(args.out, { recursive: true })

/** Whole-tree RSS in MB, sampled from `ps`: the kernel is one process, but a bash call is a child. */
function treeRssMb(rootPid: number): Promise<number> {
  return new Promise((resolve) => {
    execFile('ps', ['-Ao', 'pid=,ppid=,rss='], { maxBuffer: 32 * 1024 * 1024 }, (error, stdout) => {
      if (error) return resolve(0)
      const rows: { pid: number; ppid: number; rss: number }[] = []
      for (const line of stdout.split('\n')) {
        const match = line.trim().match(/^(\d+)\s+(\d+)\s+(\d+)$/)
        if (match)
          rows.push({ pid: Number(match[1]), ppid: Number(match[2]), rss: Number(match[3]) })
      }
      const children = new Map<number, number[]>()
      for (const row of rows) {
        const list = children.get(row.ppid) ?? []
        list.push(row.pid)
        children.set(row.ppid, list)
      }
      const rss = new Map(rows.map((row) => [row.pid, row.rss]))
      const seen = new Set<number>()
      const stack = [rootPid]
      let kb = 0
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
  pid: number
  /** Resolves when the process exits. */
  exited: Promise<number | null>
  stop: () => Promise<void>
  rss: () => Promise<number>
}

/**
 * Start the kernel under measurement. Without `--executable` it runs `src/main.ts` with Bun, which is the
 * same code the binary holds; with one it measures the product a user gets.
 */
async function startKernelProcess(provider: FakeProvider, dataDir: string): Promise<Running> {
  const work = mkdtempSync(join(tmpdir(), 'kernel-bench-work-'))
  const configPath = join(work, 'config.json')
  writeFileSync(
    configPath,
    JSON.stringify(
      {
        providers: [
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
        ],
      },
      null,
      2,
    ),
  )
  const command =
    args.executable === undefined
      ? [process.execPath, new URL('./src/main.ts', import.meta.url).pathname]
      : [args.executable]
  const child = spawn(
    command[0]!,
    [...command.slice(1), '--data', dataDir, '--port', '0', '--config', configPath],
    {
      env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN, FAKE_PROVIDER_KEY: 'fake-key' },
    },
  )
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
  const exited = new Promise<number | null>((resolve) => {
    if (child.exitCode !== null) resolve(child.exitCode)
    else child.on('exit', (code) => resolve(code))
  })
  return {
    client,
    pid: child.pid!,
    exited,
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

/** One turn: submit, then wait until the conversation answers, so the next step sees a settled state. */
async function turn(client: Client, id: number, input: string, step: Step): Promise<void> {
  const before = await client.call('conversation.entries', {
    id,
    query: { order: 'ascending' },
    limit: 1000,
  })
  const seen = new Set((before['items'] as { id: number }[]).map((entry) => entry.id))
  // The script is per turn: a provider with nothing left answers 'done' with no tool call, and the turn
  // would wait forever.
  provider.set('toolCall' in step ? [step, { text: 'ok' }] : [step])
  await client.call('conversation.submit', { id, input })
  const deadline = Date.now() + 120_000
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 1000,
    })
    const fresh = (page['items'] as { id: number; kind: string }[]).filter(
      (entry) => !seen.has(entry.id),
    )
    const results = fresh.filter((entry) => entry.kind === 'pi.tool-result').length
    const answers = fresh.filter((entry) => entry.kind === 'pi.assistant').length
    // A tool step needs the answer that called it and the answer after its result; a text step one.
    const wantedResults = 'toolCall' in step ? 1 : 0
    const wantedAnswers = 'toolCall' in step ? 2 : 1
    if (results >= wantedResults && answers >= wantedAnswers) return
    await Bun.sleep(5)
  }
  throw new Error(`the turn "${input}" never finished`)
}

/** The provider the current scene measures against; `turn()` scripts it. */
let provider: FakeProvider

async function sceneA(model: FakeProvider): Promise<{ readyMs: number; idleMb: number }> {
  const data = mkdtempSync(join(tmpdir(), 'kernel-bench-data-'))
  const started = performance.now()
  const running = await startKernelProcess(model, data)
  const readyMs = performance.now() - started
  await Bun.sleep(500)
  const idleMb = await running.rss()
  await running.stop()
  try {
    rmSync(data, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 })
  } catch {}
  return { readyMs: Math.round(readyMs), idleMb }
}

async function sceneB(model: FakeProvider, sessions: number): Promise<{ mb: number }> {
  const data = mkdtempSync(join(tmpdir(), 'kernel-bench-data-'))
  const running = await startKernelProcess(model, data)
  const ids: number[] = []
  for (let index = 0; index < sessions; index++) {
    const created = await running.client.call('conversation.create', {
      agent: { cwd: process.cwd(), model: { provider: 'fake', modelId: 'fake-1' } },
    })
    ids.push(created['conversationId'] as number)
  }
  for (const id of ids) await turn(running.client, id, `hello ${id}`, { text: 'hi there' })
  await Bun.sleep(1000)
  const mb = await running.rss()
  await running.stop()
  try {
    rmSync(data, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 })
  } catch {}
  return { mb }
}

async function sceneRounds(
  model: FakeProvider,
  tool: 'read' | 'bash',
  rounds: number,
): Promise<{ seconds: number; peakMb: number }> {
  const data = mkdtempSync(join(tmpdir(), 'kernel-bench-data-'))
  const imagePath = join(data, 'big.png')
  writeFileSync(imagePath, gradientPng(64, 64))
  const running = await startKernelProcess(model, data)
  const created = await running.client.call('conversation.create', {
    agent: { cwd: process.cwd(), model: { provider: 'fake', modelId: 'fake-1' } },
  })
  const id = created['conversationId'] as number
  let peakMb = 0
  const sampler = setInterval(() => {
    void running.rss().then((mb) => {
      peakMb = Math.max(peakMb, mb)
    })
  }, 100)
  const started = performance.now()
  for (let round = 0; round < rounds; round++) {
    // `read` on a small file: the text path, which is what C-read measures.
    const step: Step =
      tool === 'read'
        ? { toolCall: { name: 'read', arguments: { path: imagePath } } }
        : { toolCall: { name: 'bash', arguments: { command: `echo round-${round}` } } }
    await turn(running.client, id, `round ${round}`, step)
  }
  const seconds = (performance.now() - started) / 1000
  clearInterval(sampler)
  await running.stop()
  try {
    rmSync(data, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 })
  } catch {}
  return { seconds: Math.round(seconds * 100) / 100, peakMb }
}

async function main(): Promise<void> {
  provider = startFakeProvider()

  const results: Record<string, number>[] = []
  const report: string[] = [
    `# The kernel's own numbers`,
    '',
    `${new Date().toISOString()}, ${process.platform} ${process.arch}, measured the way \`shared/perf/\` measures Pi:`,
    `a local fake model, one process tree, medians of ${args.runs}.`,
    '',
    '| scene | median |',
    '| --- | ---: |',
  ]

  const cold: { readyMs: number; idleMb: number }[] = []
  for (let run = 0; run < args.runs; run++) cold.push(await sceneA(provider))
  const ready = median(cold.map((value) => value.readyMs))
  const idle = median(cold.map((value) => value.idleMb))
  report.push(`| A: start to handshake | ${ready.toFixed(0)} ms |`)
  report.push(`| A: idle RSS | ${idle.toFixed(1)} MB |`)
  results.push({ ready, idle })

  for (const sessions of [1, 5, 10]) {
    const samples: number[] = []
    for (let run = 0; run < args.runs; run++) samples.push((await sceneB(provider, sessions)).mb)
    const mb = median(samples)
    report.push(
      `| B: RSS with ${sessions} conversation${sessions === 1 ? '' : 's'} | ${mb.toFixed(1)} MB |`,
    )
    results.push({ sessions, mb })
  }

  for (const tool of ['read', 'bash'] as const) {
    const samples: { seconds: number; peakMb: number }[] = []
    for (let run = 0; run < args.runs; run++) samples.push(await sceneRounds(provider, tool, 100))
    const seconds = median(samples.map((value) => value.seconds))
    const peak = median(samples.map((value) => value.peakMb))
    report.push(`| C: 100 ${tool} rounds | ${seconds.toFixed(2)} s |`)
    report.push(`| C: peak RSS during 100 ${tool} rounds | ${peak.toFixed(1)} MB |`)
    results.push({ tool: tool === 'read' ? 1 : 2, seconds, peak })
  }

  provider.stop()
  writeFileSync(join(args.out, 'kernel-bench.md'), report.join('\n') + '\n')
  writeFileSync(
    join(args.out, 'kernel-bench.json'),
    JSON.stringify(
      { platform: process.platform, arch: process.arch, runs: args.runs, results },
      null,
      2,
    ),
  )
  process.stdout.write(report.join('\n') + '\n')
}

await main()
