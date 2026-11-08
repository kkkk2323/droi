// A Fake Kernel per test: the production Kernel against a scripted model (model.ts), behind the real Gateway
// (apps/native/internal/phonegateway), each in a throwaway world of its own: a temporary HOME, data folder,
// workspace and Scratch folder, and none of the parent's keys. The phone pairs with the Gateway as it pairs
// with a computer; the test drives the Kernel directly too, as the computer's window would.
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { createInterface } from 'node:readline'
import { fileURLToPath } from 'node:url'
import { WebSocket } from 'ws'
import type { EntryDraft } from './history'
import { MODEL_ID, PROVIDER_ID, ScriptedModel, type Completion, type Step } from './model'

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, '../../..')

/** Every model's context window. */
export const CONTEXT_WINDOW = 200_000

/** Where global-setup.ts builds the Gateway. */
export const GATEWAY_BIN = join(
  here,
  '..',
  '.bin',
  process.platform === 'win32' ? 'phonegateway.exe' : 'phonegateway',
)

/** The computer's id in /meta (phonegateway's default). */
export const COMPUTER_ID = 'c0ffee00-0000-4000-8000-000000000000'

/** The version the Gateway's /meta answers: this release's, as a computer running it would. */
export const APP_VERSION = (
  JSON.parse(readFileSync(join(repo, 'apps/native/mygo.json'), 'utf8')) as { version: string }
).version

export interface SeedSession {
  title?: string
  /**
   * The folder it works in: a name is a folder of that name the test makes (`acme-web`), or for a `scratch`
   * kind one in the Gateway's Scratch folder; an absolute path is taken as it is, and absent is the test's
   * workspace.
   */
  cwd?: string
  kind?: string
  archived?: boolean
  entries?: EntryDraft[]
  /** One of the Scenario's model ids; the fake one when absent. */
  model?: string
  thinkingLevel?: string
  /** Its own approval level, over the Host's. */
  autonomy?: Autonomy
  /** Makes its folder a git repository (once, for every seed in it). */
  git?: SeedRepo
}

export interface SeedRepo {
  /** The branch checked out; `main` when absent. */
  branch?: string
  /** Other branches, made at the first commit. */
  branches?: string[]
  /** Files in the first commit, by path. */
  committed?: Record<string, string>
  /** Files written after it and left uncommitted. */
  changed?: Record<string, string>
}

/** A throwaway repository's git, with an identity of its own as the repository's Go tests give theirs. */
function gitIn(dir: string, ...args: string[]): void {
  execFileSync('git', args, { cwd: dir, stdio: 'pipe' })
}

function writeFiles(dir: string, files: Record<string, string> = {}): void {
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(dirname(join(dir, path)), { recursive: true })
    writeFileSync(join(dir, path), content)
  }
}

function makeRepo(dir: string, seed: SeedRepo): void {
  gitIn(dir, 'init', '-q', '-b', seed.branch ?? 'main')
  gitIn(dir, 'config', 'user.email', 't@example.com')
  gitIn(dir, 'config', 'user.name', 'T')
  writeFiles(dir, seed.committed)
  gitIn(dir, 'add', '-A')
  gitIn(dir, 'commit', '-q', '--allow-empty', '-m', 'first')
  for (const branch of seed.branches ?? []) gitIn(dir, 'branch', branch)
  writeFiles(dir, seed.changed)
}

/** A model the computer offers besides the fake one; every model answers from the same script. */
export interface ScenarioModel {
  id: string
  name?: string
  /** Its thinking levels; ['off'] when absent. */
  thinking?: string[]
}

type Autonomy = 'ask' | 'edits' | 'safe' | 'all'

export interface Scenario {
  steps?: Step[]
  completions?: Completion[]
  /** The approval level the Host sets; "all" when absent, so a tool call runs without a card. */
  autonomy?: Autonomy
  /** The models the computer offers before the fake one, in this order. */
  models?: ScenarioModel[]
  /** The Session defaults' model, which the computer's window gives the Kernel; the fake one when absent, null for none. */
  defaultModel?: string | null
  /** Sessions the Kernel holds before the phone connects, in this order. */
  sessions?: SeedSession[]
  /** The computer's window and its Session list order (a Gateway without one answers 404). */
  window?: {
    pinnedSessions?: string[]
    pinnedWorkspaces?: string[]
    workspaceOrder?: string[]
    sessionSort?: string
    sessionView?: string
  }
  /** The computer's name in /meta. */
  name?: string
  /** The Droi version /meta answers; this release's when absent. */
  version?: string
  /** The computer's id in /meta; a second computer in one test needs its own. */
  computerId?: string
  /** Skills: the user's own, or with `cwd` that folder's (`.factory/skills`). */
  skills?: ScenarioSkill[]
  /** The user's prompt templates, the commands "/" offers. */
  prompts?: Array<{ name: string; description?: string; body?: string }>
  /** The user's mcp.json servers, by name, as the file holds them; `fakeMcpServer` makes a command entry. */
  mcpServers?: Record<string, Record<string, unknown>>
  /** A folder's own `.factory/mcp.json` servers, by the folder's name and then the server's. */
  repoMcpServers?: Record<string, Record<string, Record<string, unknown>>>
}

export interface ScenarioSkill {
  name: string
  description: string
  /** What the Kernel puts in the prompt when "/name" runs it. */
  body?: string
  /** A folder's own skill, by the folder's name as a seed names it. */
  cwd?: string
  /** Only a person starts it ("disable-model-invocation"). */
  hidden?: boolean
}

/** The stdio MCP server the Scenario's `mcpServers` can run, with these tools. */
export function fakeMcpServer(tools: string[]): Record<string, unknown> {
  return { command: 'node', args: [join(here, 'mcp-server.mjs')], env: { TOOLS: tools.join(',') } }
}

/** A folder of a test's own by name (made if need be), or an absolute path as it is. */
function folderIn(root: string, name?: string): string {
  if (!name) return join(root, 'workspace')
  if (isAbsolute(name)) return name
  const dir = join(root, 'folders', name)
  mkdirSync(dir, { recursive: true })
  return dir
}

/** The files the Scenario puts where the Kernel reads them, before it starts. */
function writeResources(root: string, scenario: Scenario): void {
  const user = join(root, 'user')
  for (const skill of scenario.skills ?? []) {
    const base = skill.cwd
      ? join(folderIn(root, skill.cwd), '.factory', 'skills')
      : join(user, 'skills')
    mkdirSync(join(base, skill.name), { recursive: true })
    const front = [
      `name: ${skill.name}`,
      `description: ${JSON.stringify(skill.description)}`,
      ...(skill.hidden ? ['disable-model-invocation: true'] : []),
    ]
    writeFileSync(
      join(base, skill.name, 'SKILL.md'),
      `---\n${front.join('\n')}\n---\n${skill.body ?? `Do ${skill.name}.`}\n`,
    )
  }
  for (const prompt of scenario.prompts ?? []) {
    mkdirSync(join(user, 'prompts'), { recursive: true })
    const front = prompt.description
      ? `---\ndescription: ${JSON.stringify(prompt.description)}\n---\n`
      : ''
    writeFileSync(
      join(user, 'prompts', `${prompt.name}.md`),
      `${front}${prompt.body ?? `Run ${prompt.name}: $ARGUMENTS`}\n`,
    )
  }
  if (scenario.mcpServers) {
    writeFileSync(
      join(user, 'mcp.json'),
      JSON.stringify({ mcpServers: scenario.mcpServers }, null, 2),
    )
  }
  for (const [folder, servers] of Object.entries(scenario.repoMcpServers ?? {})) {
    const dir = join(folderIn(root, folder), '.factory')
    mkdirSync(dir, { recursive: true })
    writeFileSync(join(dir, 'mcp.json'), JSON.stringify({ mcpServers: servers }, null, 2))
  }
}

/**
 * The Kernel to run: DROI_KERNEL_BIN (the CI's build), or its source under Bun. Not a build lying in
 * apps/native/assets/kernel: one older than the source would test what the source no longer says.
 */
function kernelCommand(): { command: string; args: string[] } {
  const given = process.env['DROI_KERNEL_BIN']
  if (given) return { command: given, args: [] }
  return { command: 'bun', args: ['run', join(repo, 'apps/kernel/src/main.ts')] }
}

/** The environment a test's Kernel and its tools get: a whitelist, and a HOME of their own. */
function isolatedEnv(home: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {}
  for (const key of [
    'PATH',
    'SHELL',
    'LANG',
    'LC_ALL',
    'LC_CTYPE',
    'TERM',
    'TZ',
    'SystemRoot',
    'SystemDrive',
    'windir',
    'COMSPEC',
    'PATHEXT',
    'ProgramFiles',
    'LOCALAPPDATA',
  ]) {
    const value = process.env[key]
    if (value) env[key] = value
  }
  const temp = join(home, 'tmp')
  mkdirSync(temp, { recursive: true })
  return { ...env, HOME: home, USERPROFILE: home, TMPDIR: temp, TMP: temp, TEMP: temp }
}

function firstLine(child: ChildProcess, what: string, logs: () => string): Promise<string> {
  return new Promise((resolveLine, reject) => {
    const lines = createInterface({ input: child.stdout! })
    const timer = setTimeout(
      () => reject(new Error(`${what} did not start in 60 s:\n${logs()}`)),
      60_000,
    )
    lines.once('line', (line) => {
      clearTimeout(timer)
      resolveLine(line)
    })
    child.once('exit', (code) => {
      clearTimeout(timer)
      reject(new Error(`${what} exited (${code}) before it started:\n${logs()}`))
    })
  })
}

/** A JSON-RPC connection to the Kernel, as the computer's window has one. */
export class KernelClient {
  #ws: WebSocket
  #next = 0
  #waiting = new Map<string, { resolve: (v: unknown) => void; reject: (e: Error) => void }>()
  readonly notifications: Array<{ method: string; params: Record<string, unknown> }> = []

  private constructor(ws: WebSocket) {
    this.#ws = ws
    ws.on('message', (data) => {
      const frame = JSON.parse(String(data)) as {
        id?: string
        result?: unknown
        error?: { code: number; message: string }
        method?: string
        params?: Record<string, unknown>
      }
      if (frame.id !== undefined) {
        const w = this.#waiting.get(String(frame.id))
        this.#waiting.delete(String(frame.id))
        if (frame.error)
          w?.reject(Object.assign(new Error(frame.error.message), { code: frame.error.code }))
        else w?.resolve(frame.result)
        return
      }
      if (frame.method)
        this.notifications.push({ method: frame.method, params: frame.params ?? {} })
    })
  }

  static async open(port: number, token: string): Promise<KernelClient> {
    const ws = new WebSocket(`ws://127.0.0.1:${port}/rpc`, {
      headers: { Authorization: `Bearer ${token}`, 'X-Droi-Client': 'window' },
      maxPayload: 64 << 20,
    })
    await new Promise<void>((ok, fail) => {
      ws.once('open', () => ok())
      ws.once('error', fail)
    })
    return new KernelClient(ws)
  }

  /** The first notification, heard so far or from now on, that `matches`. */
  async waitFor(
    matches: (note: { method: string; params: Record<string, unknown> }) => boolean,
    timeoutMs = 20_000,
  ) {
    const deadline = Date.now() + timeoutMs
    for (;;) {
      const found = this.notifications.find(matches)
      if (found) return found
      if (Date.now() > deadline) throw new Error('the window never heard what it waited for')
      await new Promise((wake) => setTimeout(wake, 50))
    }
  }

  call<T = Record<string, unknown>>(
    method: string,
    params: Record<string, unknown> = {},
  ): Promise<T> {
    const id = String(++this.#next)
    return new Promise<T>((ok, fail) => {
      this.#waiting.set(id, { resolve: ok as (v: unknown) => void, reject: fail })
      this.#ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
    })
  }

  close(): void {
    this.#ws.close()
  }
}

export interface Seeded {
  /** The Session's id, as the phone names it. */
  sessionId: string
  conversationId: number
  title: string
}

export class FakeKernel {
  readonly model: ScriptedModel
  /** The folder Sessions work in unless a seed says otherwise; a git repository it is not. */
  readonly workspace: string
  readonly scratch: string
  /** The seeded Sessions, in the Scenario's order. */
  readonly sessions: Seeded[] = []
  token: string
  url = ''
  readonly kernel: KernelClient
  readonly computerId: string
  #root: string
  #kernel: ChildProcess
  #gateway: ChildProcess | null = null
  /** The Gateway's stdout lines not read yet, and who waits for the next. */
  #lines: string[] = []
  #lineWaiters: Array<(line: string) => void> = []
  #gatewayPort = 0
  #scenario: Scenario
  #daemonOnly = false
  #answered = new Set<string>()
  #repos = new Set<string>()
  #logs = ''

  private constructor(
    root: string,
    model: ScriptedModel,
    kernel: ChildProcess,
    client: KernelClient,
    readonly kernelPort: number,
    readonly kernelToken: string,
    scenario: Scenario,
  ) {
    this.#root = root
    this.model = model
    this.#kernel = kernel
    this.kernel = client
    this.workspace = join(root, 'workspace')
    this.scratch = join(root, 'scratch')
    this.token = randomBytes(16).toString('hex')
    this.#scenario = scenario
    this.computerId = scenario.computerId ?? COMPUTER_ID
  }

  static async start(scenario: Scenario = {}): Promise<FakeKernel> {
    const root = mkdtempSync(join(tmpdir(), 'droi-phone-e2e-'))
    for (const dir of ['data', 'home', 'user', 'workspace', 'scratch', 'worktrees'])
      mkdirSync(join(root, dir))
    writeResources(root, scenario)
    const model = await ScriptedModel.start(scenario.steps, scenario.completions)
    const kernelToken = randomBytes(24).toString('hex')
    const { command, args } = kernelCommand()
    let logs = ''
    const child = spawn(
      command,
      [...args, '--data', join(root, 'data'), '--port', '0', '--host', '127.0.0.1'],
      {
        cwd: join(root, 'workspace'),
        env: {
          ...isolatedEnv(join(root, 'home')),
          DROI_KERNEL_TOKEN: kernelToken,
          DROI_USER_DIR: join(root, 'user'),
          DROI_KERNEL_PROVIDERS: JSON.stringify([
            {
              id: PROVIDER_ID,
              name: 'Fake provider',
              baseUrl: model.baseUrl,
              api: 'openai-completions',
              apiKeyEnv: 'FAKE_PROVIDER_KEY',
              models: [
                ...(scenario.models ?? []).map((m) => ({
                  id: m.id,
                  name: m.name ?? m.id,
                  contextWindow: CONTEXT_WINDOW,
                  maxTokens: 8_000,
                  input: ['text', 'image'],
                  ...(m.thinking ? { thinking: m.thinking } : {}),
                })),
                {
                  id: MODEL_ID,
                  name: 'Fake one',
                  contextWindow: CONTEXT_WINDOW,
                  maxTokens: 8_000,
                  input: ['text', 'image'],
                },
              ],
            },
          ]),
          FAKE_PROVIDER_KEY: 'fake-key',
        },
        stdio: ['ignore', 'pipe', 'pipe'],
      },
    )
    child.stderr!.on('data', (chunk) => (logs += String(chunk)))
    const handshake = JSON.parse(await firstLine(child, 'the Kernel', () => logs)) as {
      port: number
    }
    const client = await KernelClient.open(handshake.port, kernelToken)
    const fake = new FakeKernel(root, model, child, client, handshake.port, kernelToken, scenario)
    // What the computer's window gives every Kernel it connects to: the Session defaults' level and model.
    await client.call('policy.set', { policy: { autonomy: scenario.autonomy ?? 'all' } })
    const defaultModel = scenario.defaultModel === undefined ? MODEL_ID : scenario.defaultModel
    if (defaultModel !== null) {
      await client.call('harness.configure', {
        agent: { model: { provider: PROVIDER_ID, modelId: defaultModel } },
      })
    }
    for (const seed of scenario.sessions ?? []) await fake.seed(seed)
    await fake.#startGateway()
    return fake
  }

  /** Writes a Session's history into the Kernel, as one an earlier run left. */
  async seed(seed: SeedSession): Promise<Seeded> {
    const title = seed.title ?? 'Untitled session'
    const cwd =
      seed.kind === 'scratch' && seed.cwd && !isAbsolute(seed.cwd)
        ? join(this.scratch, seed.cwd)
        : this.folder(seed.cwd)
    mkdirSync(cwd, { recursive: true })
    if (seed.git && !this.#repos.has(cwd)) {
      this.#repos.add(cwd)
      makeRepo(cwd, seed.git)
    }
    const made = await this.kernel.call<{ conversationId: number }>('conversation.import', {
      entries: seed.entries ?? [],
      cwd,
      kind: seed.kind ?? 'chat',
      title,
      agent: {
        model: { provider: PROVIDER_ID, modelId: seed.model ?? MODEL_ID },
        ...(seed.thinkingLevel ? { thinkingLevel: seed.thinkingLevel } : {}),
      },
      ...(seed.archived ? { archived: true } : {}),
    })
    if (seed.autonomy)
      await this.kernel.call('policy.set', {
        id: made.conversationId,
        policy: { autonomy: seed.autonomy },
      })
    const seeded = {
      sessionId: String(made.conversationId),
      conversationId: made.conversationId,
      title,
    }
    this.sessions.push(seeded)
    return seeded
  }

  /** A folder of this test's own by name (made if need be), or an absolute path as it is. */
  folder(name?: string): string {
    return folderIn(this.#root, name)
  }

  /** The seeded Session with this title. */
  session(title: string): Seeded {
    const found = this.sessions.find((s) => s.title === title)
    if (!found) throw new Error(`no seeded Session is called ${title}`)
    return found
  }

  async #startGateway(): Promise<void> {
    const scenario = this.#scenario
    const child = spawn(
      GATEWAY_BIN,
      [
        '-port',
        String(this.#gatewayPort),
        '-token',
        this.token,
        '-kernel-url',
        `ws://127.0.0.1:${this.kernelPort}/rpc`,
        '-version',
        scenario.version ?? APP_VERSION,
        '-name',
        scenario.name ?? 'Test Mac',
        '-computer-id',
        this.computerId,
        '-scratch',
        this.scratch,
        '-worktrees',
        join(this.#root, 'worktrees'),
        ...(scenario.window ? ['-window'] : []),
        ...(this.#daemonOnly ? ['-daemon-only'] : []),
      ],
      {
        env: { ...process.env, DROI_KERNEL_TOKEN: this.kernelToken },
        stdio: ['pipe', 'pipe', 'pipe'],
      },
    )
    child.stderr!.on('data', (chunk) => (this.#logs += String(chunk)))
    this.#lines = []
    this.#lineWaiters = []
    createInterface({ input: child.stdout! }).on('line', (line) => {
      const waiter = this.#lineWaiters.shift()
      if (waiter) waiter(line)
      else this.#lines.push(line)
    })
    const ready = JSON.parse(await this.#nextLine(child)) as { url: string }
    this.#gateway = child
    this.url = ready.url
    this.#gatewayPort = Number(new URL(ready.url).port)
    if (scenario.window)
      await this.#command(
        `list ${JSON.stringify({ pinnedSessions: [], pinnedWorkspaces: [], workspaceOrder: [], sessionSort: '', sessionView: '', ...scenario.window })}`,
      )
  }

  #nextLine(child: ChildProcess): Promise<string> {
    const queued = this.#lines.shift()
    if (queued !== undefined) return Promise.resolve(queued)
    return new Promise((ok, fail) => {
      const timer = setTimeout(
        () => fail(new Error(`the Gateway did not answer:\n${this.#logs}`)),
        60_000,
      )
      const exit = (code: number | null) => {
        clearTimeout(timer)
        fail(new Error(`the Gateway exited (${code}):\n${this.#logs}`))
      }
      child.once('exit', exit)
      this.#lineWaiters.push((line) => {
        clearTimeout(timer)
        child.off('exit', exit)
        ok(line)
      })
    })
  }

  async #command(line: string): Promise<void> {
    const gateway = this.#gateway
    if (!gateway) return
    gateway.stdin!.write(`${line}\n`)
    await this.#nextLine(gateway)
  }

  /** Sends a message to a seeded Session from the computer's window, as if typed there. */
  async submit(title: string, input: string): Promise<void> {
    await this.kernel.call('conversation.submit', { id: this.session(title).conversationId, input })
  }

  /** Resolves once a seeded Session's run, which the window saw start, has ended. */
  async settled(title: string): Promise<void> {
    const id = this.session(title).conversationId
    const busy = () =>
      this.kernel.notifications.filter(
        (n) => n.method === 'conversation.busy' && n.params['id'] === id,
      )
    await this.kernel.waitFor(
      () => busy().some((n) => n.params['run'] !== null) && busy().at(-1)!.params['run'] === null,
    )
  }

  /** The computer's window answers the approval a seeded Session waits on (its first not yet answered). */
  async approve(title: string, outcome = 'proceed_once'): Promise<void> {
    const id = this.session(title).conversationId
    const note = await this.kernel.waitFor(
      (n) =>
        n.method === 'approval.requested' &&
        n.params['id'] === id &&
        !this.#answered.has(String(n.params['requestId'])),
    )
    const requestId = String(note.params['requestId'])
    this.#answered.add(requestId)
    await this.kernel.call('approval.respond', { requestId, outcome })
  }

  /** The computer's window sends a message whose answer the model streams as `step` says. */
  async turn(title: string, input: string, step: Omit<Step, 'match'> = {}): Promise<void> {
    this.model.push({ ...step, match: input })
    await this.submit(title, input)
  }

  /** A new Pairing Token, as Settings' reset makes: phones paired with the old one are dropped. */
  async resetToken(): Promise<string> {
    this.token = randomBytes(16).toString('hex')
    await this.#command(`token ${this.token}`)
    return this.token
  }

  /** The computer goes away: its Gateway stops answering. */
  async goDown(): Promise<void> {
    const gateway = this.#gateway
    this.#gateway = null
    if (!gateway) return
    const exited = new Promise((ok) => gateway.once('exit', ok))
    gateway.stdin!.end()
    gateway.kill()
    await exited
  }

  /** It comes back at the same address. */
  async comeBack(): Promise<void> {
    await this.#startGateway()
  }

  /** The computer switches to Daemon mode: its Gateway comes back with no Kernel behind it. */
  async withoutKernel(): Promise<void> {
    await this.goDown()
    this.#daemonOnly = true
    await this.#startGateway()
  }

  /** It switches back to its Kernel. */
  async withKernel(): Promise<void> {
    await this.goDown()
    this.#daemonOnly = false
    await this.#startGateway()
  }

  async stop(): Promise<void> {
    await this.goDown()
    try {
      await Promise.race([
        this.kernel.call('kernel.shutdown'),
        new Promise((ok) => setTimeout(ok, 3_000)),
      ])
    } catch {
      // It may have gone already.
    }
    this.kernel.close()
    const kernel = this.#kernel
    if (kernel.exitCode === null) {
      const exited = new Promise((ok) => kernel.once('exit', ok))
      const timer = setTimeout(() => kernel.kill('SIGKILL'), 10_000)
      await exited
      clearTimeout(timer)
    }
    await this.model.stop()
    rmSync(this.#root, { recursive: true, force: true })
  }
}
