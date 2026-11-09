import { readFileSync } from 'node:fs'
import type { Policy } from './policy/decide.ts'
import { isCodemodeMode, type CodemodeMode } from './extensions/codemode.ts'
import { homedir } from 'node:os'
import { isAbsolute, join } from 'node:path'

/** One provider the Host points the kernel at, and the models it may use. */
export type ProviderConfig = {
  id: string
  name?: string
  /** Where the provider is: for Factory, the Host's own LLM proxy. */
  baseUrl?: string
  /** pi-ai API of every model here: `openai-completions`, `openai-responses`, `anthropic-messages`, `google-generative-ai`. */
  api: string
  /** Environment variable the api key is read from. The key never crosses the wire. */
  apiKeyEnv?: string
  headers?: Record<string, string>
  models: ModelConfig[]
}

/** One model the Host offers, in the shape pi-ai's catalog takes. */
export type ModelConfig = {
  id: string
  name?: string
  contextWindow: number
  maxTokens: number
  input?: ('text' | 'image')[]
  reasoning?: boolean
  /** Dollars per million tokens, as pi-ai's catalog states them. */
  cost?: { input: number; output: number; cacheRead?: number; cacheWrite?: number }
  /** The thinking levels the model takes, for `harness.info.models[].thinking`. */
  thinking?: string[]
  /**
   * pi-ai's: each thinking level as the provider's value, null for one the model refuses. Without an entry pi-ai
   * sends xhigh and max as high.
   */
  thinkingLevelMap?: Partial<Record<string, string | null>>
  /** pi-ai's compatibility settings for the model's api, such as Claude's `forceAdaptiveThinking`. */
  compat?: Record<string, unknown>
}

export type KernelConfig = {
  port: number
  host: string
  dataDir: string
  /**
   * The user's own directory: personal skills, prompt templates and `AGENTS.md`, and later the global
   * `mcp.json`. The Host passes it in `DROI_USER_DIR` because the brand decides the path; without it the
   * kernel uses `~/.droi`, so a kernel started by hand still finds them.
   */
  userDir: string
  /**
   * The Memory folder when Memory is on (`DROI_MEMORY_DIR`, ADR 0042): the Memory tools and the Memory Summary in
   * each conversation's prompt. Without it a conversation has neither.
   */
  memoryDir?: string
  readOnly: boolean
  providers: ProviderConfig[]
  /**
   * What a new conversation starts with when the request names nothing: the config file's `agent`. Without
   * it a conversation has no model, and its first run ends `no_model` without calling a provider at all —
   * which is every new conversation in a window that does not send a model per create.
   */
  agent?: AgentDefaults
  /** How often running progress is committed; Pi's defaults when absent. */
  progress?: { partialIntervalMs: number; outputIntervalMs: number; persistWithinMs: number }
  /**
   * An approval policy known at start, so calls are judged before the Host pushes one. Without it nothing above
   * `ask` runs until the first `policy.set`, or until `policyWaitMs` (30 s) settles the level at `ask`.
   */
  policy?: Policy
  policyWaitMs?: number
  /** How long an approval card waits for a person: ten minutes unless a test says otherwise. */
  approvalTimeoutMs?: number
  /** Pushes a second a connection gets per conversation; the contract says at most 5. */
  viewPushesPerSecond?: number
  /** What the brand's app gives every conversation (`DROI_KERNEL_AGENT`, ADR 0041). */
  app?: AppAgent
  /**
   * How codemode presents tools (`DROI_KERNEL_CODEMODE`, or the config file's `codemode.mode`): `on` unless set.
   * `harness.configure {codemode: {mode}}` changes it for every conversation's next request.
   */
  codemodeMode?: CodemodeMode
  /**
   * The model that writes every compaction's summary (`DROI_KERNEL_COMPACTION_MODEL` as `provider/modelId`, or the
   * config file's `compaction.model`); unset, each conversation's own. `harness.configure {compaction: {model}}`
   * changes it for the next compaction.
   */
  compactionModel?: { provider: string; modelId: string }
}

/**
 * The brand's app's part of every conversation (ADR 0041), as the Host gives it at launch: MCP servers beside the
 * user's, the tools it lets run without asking, and a prompt section. `harness.configure` changes the section.
 */
export type AppAgent = {
  /** By name, in `mcp.json`'s entry shape. They are not the user's: `mcp.set` neither lists them as editable nor writes them. */
  mcpServers: Record<string, Record<string, unknown>>
  /** The servers a turn waits for, up to their first connection's end, before it starts. */
  blockOnLoad: string[]
  /** Kernel tool names (`<server>___<tool>`) the approval policy lets run, after the user's own rules. */
  alwaysAllow: string[]
  section?: string
}

/** `DROI_KERNEL_AGENT`: `{mcpServers: {name: {…, blockOnLoad?}}, alwaysAllow: [], section}`, every field optional. */
export function appAgentFromEnv(env: NodeJS.ProcessEnv = process.env): AppAgent | undefined {
  const raw = env['DROI_KERNEL_AGENT']
  if (raw === undefined || raw.trim() === '') return undefined
  const parsed: unknown = JSON.parse(raw)
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('DROI_KERNEL_AGENT must hold a JSON object')
  }
  const value = parsed as Record<string, unknown>
  const app: AppAgent = { mcpServers: {}, blockOnLoad: [], alwaysAllow: [] }
  const servers = value['mcpServers'] ?? {}
  if (typeof servers !== 'object' || servers === null || Array.isArray(servers)) {
    throw new Error('DROI_KERNEL_AGENT.mcpServers must be an object')
  }
  for (const [name, entry] of Object.entries(servers as Record<string, unknown>)) {
    if (typeof entry !== 'object' || entry === null || Array.isArray(entry)) {
      throw new Error(`DROI_KERNEL_AGENT.mcpServers.${name} must be an object`)
    }
    const { blockOnLoad, ...rest } = entry as Record<string, unknown>
    app.mcpServers[name] = rest
    if (blockOnLoad === true) app.blockOnLoad.push(name)
  }
  const allow = value['alwaysAllow'] ?? []
  if (!Array.isArray(allow) || allow.some((tool) => typeof tool !== 'string' || tool === '')) {
    throw new Error('DROI_KERNEL_AGENT.alwaysAllow must be a list of tool names')
  }
  app.alwaysAllow = allow as string[]
  const section = value['section']
  if (section !== undefined && section !== null) {
    if (typeof section !== 'string') throw new Error('DROI_KERNEL_AGENT.section must be a string')
    if (section.trim() !== '') app.section = section
  }
  return app
}

/** The config file's `agent`: the defaults a new conversation inherits. */
export type AgentDefaults = {
  provider?: string
  modelId?: string
  thinkingLevel?: string
  cwd?: string
  instructions?: string
}

export type Args = {
  port: number
  host: string
  dataDir: string
  readOnly: boolean
  configPath?: string
}

/** The only interfaces a kernel may bind: everything it serves is for this computer. */
const LOOPBACK = new Set(['127.0.0.1', '::1', 'localhost', '[::1]'])

const usage = `usage: kernel --data <dir> [--port 0] [--host 127.0.0.1] [--config <file>] [--read-only]

The token arrives in DROI_KERNEL_TOKEN: on macOS ps shows every user's process
arguments, so a token on the command line is readable by any local user.`

/** Parse the command line. Throws a sentence for the user on a bad one. */
export function parseArgs(argv: readonly string[]): Args {
  let port = 0
  let host = '127.0.0.1'
  let dataDir: string | undefined
  let readOnly = false
  let configPath: string | undefined
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]!
    const next = () => {
      const value = argv[++i]
      if (value === undefined) throw new Error(`${arg} needs a value`)
      return value
    }
    switch (arg) {
      case '--port':
        port = Number(next())
        if (!Number.isInteger(port) || port < 0 || port > 65535)
          throw new Error(`--port must be 0 to 65535`)
        break
      case '--host':
        host = next()
        // A kernel holds a token and the whole transcript of its conversations. It may listen on this
        // computer and nowhere else: a Host that wants the phone to reach it has the Gateway for that.
        if (!LOOPBACK.has(host)) {
          throw new Error(`--host must be a loopback address (127.0.0.1, ::1), not ${host}`)
        }
        break
      case '--data':
        dataDir = next()
        break
      case '--config':
        configPath = next()
        break
      case '--read-only':
        readOnly = true
        break
      case '--help':
      case '-h':
        throw new Error(usage)
      default:
        throw new Error(`unknown argument: ${arg}\n${usage}`)
    }
  }
  if (dataDir === undefined) throw new Error(`--data is required\n${usage}`)
  return { port, host, dataDir, readOnly, configPath }
}

/**
 * Providers from `DROI_KERNEL_PROVIDERS`: a JSON array, the same shape as the config file's
 * `providers`. It exists so a test, a CI job or the Fake Daemon can point the kernel at a local model
 * without writing a file — and it changes nothing else: the token still gates every connection, and the
 * data directory, the port and the read-only flag come from the command line.
 */
export function providersFromEnv(
  env: NodeJS.ProcessEnv = process.env,
): ProviderConfig[] | undefined {
  const raw = env['DROI_KERNEL_PROVIDERS']
  if (raw === undefined || raw.trim() === '') return undefined
  const parsed: unknown = JSON.parse(raw)
  if (!Array.isArray(parsed) || parsed.length === 0) {
    throw new Error('DROI_KERNEL_PROVIDERS must hold a non-empty JSON array of providers')
  }
  return validateProviders(parsed)
}

export function validateProviders(value: unknown): ProviderConfig[] {
  if (!Array.isArray(value)) throw new Error('providers must be an array')
  for (const provider of value) {
    if (typeof provider !== 'object' || provider === null)
      throw new Error('every provider must be an object')
    const candidate = provider as Partial<ProviderConfig>
    if (typeof candidate.id !== 'string' || candidate.id === '')
      throw new Error('every provider needs an id')
    if (typeof candidate.api !== 'string' || candidate.api === '') {
      throw new Error(`provider ${candidate.id} needs an api`)
    }
    if (!Array.isArray(candidate.models)) throw new Error(`provider ${candidate.id} needs models`)
  }
  return value as ProviderConfig[]
}

function parseAgentDefaults(value: unknown): AgentDefaults | undefined {
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'object') throw new Error('agent must be an object')
  const raw = value as Record<string, unknown>
  const string = (key: string): string | undefined => {
    const field = raw[key]
    if (field === undefined || field === null) return undefined
    if (typeof field !== 'string') throw new Error(`agent.${key} must be a string`)
    return field
  }
  // Both `{agent: {provider, modelId}}` and `{agent: {model: {provider, modelId}}}` are accepted: the
  // model picks the first shape, the protocol's agent change the second.
  const nested = raw['model']
  const provider =
    string('provider') ??
    (typeof nested === 'object' && nested !== null
      ? (nested as { provider?: string }).provider
      : undefined)
  const modelId =
    string('modelId') ??
    (typeof nested === 'object' && nested !== null
      ? ((nested as { modelId?: string }).modelId ?? (nested as { id?: string }).id)
      : undefined)
  return {
    ...(provider === undefined ? {} : { provider }),
    ...(modelId === undefined ? {} : { modelId }),
    ...(string('thinkingLevel') === undefined ? {} : { thinkingLevel: string('thinkingLevel') }),
    ...(string('cwd') === undefined ? {} : { cwd: string('cwd') }),
    ...(string('instructions') === undefined ? {} : { instructions: string('instructions') }),
  }
}

/**
 * The user's own directory: `DROI_USER_DIR` when the Host set it, else `~/.droi` — the brand's folder,
 * which is also where the Host keeps its scratch conversations. A relative path is refused: the kernel's own
 * working directory is not a place to write files into.
 */
export function userDirFromEnv(env: NodeJS.ProcessEnv = process.env): string {
  const given = env['DROI_USER_DIR']
  if (given === undefined || given.trim() === '') return join(homedir(), '.droi')
  if (!isAbsolute(given)) throw new Error(`DROI_USER_DIR must be an absolute path, not ${given}`)
  return given
}
/** The Memory folder from `DROI_MEMORY_DIR`, undefined when Memory is off; a relative path is refused. */
export function memoryDirFromEnv(env: NodeJS.ProcessEnv = process.env): string | undefined {
  const given = env['DROI_MEMORY_DIR']
  if (given === undefined || given.trim() === '') return undefined
  if (!isAbsolute(given)) throw new Error(`DROI_MEMORY_DIR must be an absolute path, not ${given}`)
  return given
}

/** `DROI_KERNEL_CODEMODE`, else the config file's `codemode.mode`: `on` or `only`; undefined for codemode's default. */
export function codemodeModeFrom(
  file: unknown,
  env: NodeJS.ProcessEnv = process.env,
): CodemodeMode | undefined {
  const given = env['DROI_KERNEL_CODEMODE']
  if (given !== undefined && given.trim() !== '') {
    if (!isCodemodeMode(given))
      throw new Error(`DROI_KERNEL_CODEMODE must be on or only, not ${given}`)
    return given
  }
  const mode = (file as { mode?: unknown } | undefined)?.mode
  if (mode === undefined || mode === null) return undefined
  if (!isCodemodeMode(mode)) throw new Error('codemode.mode must be on or only')
  return mode
}

/**
 * `DROI_KERNEL_COMPACTION_MODEL` (`provider/modelId`: a provider id has no `/`, a model id may), else the config
 * file's `compaction.model` (`{provider, modelId}`); undefined for each conversation's own model.
 */
export function compactionModelFrom(
  file: unknown,
  env: NodeJS.ProcessEnv = process.env,
): { provider: string; modelId: string } | undefined {
  const given = env['DROI_KERNEL_COMPACTION_MODEL']
  if (given !== undefined && given.trim() !== '') {
    const at = given.indexOf('/')
    if (at <= 0 || at === given.length - 1)
      throw new Error(`DROI_KERNEL_COMPACTION_MODEL must be provider/modelId, not ${given}`)
    return { provider: given.slice(0, at), modelId: given.slice(at + 1) }
  }
  const model = (file as { model?: unknown } | undefined)?.model
  if (model === undefined || model === null) return undefined
  const ref = modelRefOf(model)
  if (ref === undefined) throw new Error('compaction.model must be {provider, modelId}')
  return ref
}

/** `{provider, modelId}` with two non-empty strings, else undefined. */
export function modelRefOf(value: unknown): { provider: string; modelId: string } | undefined {
  if (typeof value !== 'object' || value === null) return undefined
  const { provider, modelId } = value as { provider?: unknown; modelId?: unknown }
  if (
    typeof provider !== 'string' ||
    provider === '' ||
    typeof modelId !== 'string' ||
    modelId === ''
  )
    return undefined
  return { provider, modelId }
}

/** Read the config file, or the empty config when there is none. */
export function loadConfig(args: Args, env: NodeJS.ProcessEnv = process.env): KernelConfig {
  let file: Partial<KernelConfig> = {}
  if (args.configPath !== undefined) {
    const parsed: unknown = JSON.parse(readFileSync(args.configPath, 'utf8'))
    if (typeof parsed !== 'object' || parsed === null)
      throw new Error('the config file must hold a JSON object')
    file = parsed as Partial<KernelConfig>
  }
  const fromEnv = providersFromEnv(env)
  const memoryDir = memoryDirFromEnv(env)
  const app = appAgentFromEnv(env)
  const codemodeMode = codemodeModeFrom((file as { codemode?: unknown }).codemode, env)
  const compactionModel = compactionModelFrom((file as { compaction?: unknown }).compaction, env)
  return {
    port: args.port,
    host: args.host,
    dataDir: args.dataDir,
    userDir: userDirFromEnv(env),
    ...(memoryDir === undefined ? {} : { memoryDir }),
    readOnly: args.readOnly,
    // The environment wins: it exists for a test that has no file to write.
    providers: fromEnv ?? validateProviders(file.providers ?? []),
    agent: parseAgentDefaults(file.agent),
    progress: file.progress,
    viewPushesPerSecond: file.viewPushesPerSecond ?? 5,
    ...(app === undefined ? {} : { app }),
    ...(codemodeMode === undefined ? {} : { codemodeMode }),
    ...(compactionModel === undefined ? {} : { compactionModel }),
  }
}
