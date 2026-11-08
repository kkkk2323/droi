import { readFileSync } from 'node:fs'
import type { Policy } from './policy/decide.ts'

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
}

export type KernelConfig = {
  port: number
  host: string
  dataDir: string
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

function validateProviders(value: unknown): ProviderConfig[] {
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
  return {
    port: args.port,
    host: args.host,
    dataDir: args.dataDir,
    readOnly: args.readOnly,
    // The environment wins: it exists for a test that has no file to write.
    providers: fromEnv ?? validateProviders(file.providers ?? []),
    agent: parseAgentDefaults(file.agent),
    progress: file.progress,
    viewPushesPerSecond: file.viewPushesPerSecond ?? 5,
  }
}
