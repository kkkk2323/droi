import { isAbsolute, join, resolve } from 'node:path'
import type { Context } from '@earendil-works/chord'
import { AgentDoc } from '@earendil-works/pi-durable'
import type { Conversation, Extension, Harness } from '@earendil-works/pi-durable'
import { invalidParams, optionalBoolean, optionalObject, optionalString } from '../protocol/rpc.ts'
import { repoRootOf } from '../workspace.ts'
import { readConfig, withoutConnection, writeEntry, type RawEntry } from './config.ts'
import type { McpServers } from './servers.ts'
import type { McpAuth } from './oauth.ts'

/** `mcp.list {repoRoot?}`. */
export async function mcpList(
  servers: McpServers,
  params: Record<string, unknown>,
): Promise<unknown> {
  const root = await rootParam(params)
  await servers.refreshGlobal()
  if (root !== undefined) await servers.refreshRepo(root)
  return servers.list(root)
}

/**
 * `mcp.set {name, server?, enabled?, remove?, repoRoot?, trust?}`: a change to the global file, taking effect at
 * once, or the user's answer about a repository's server.
 */
export async function mcpSet(
  servers: McpServers,
  userDir: string,
  params: Record<string, unknown>,
): Promise<unknown> {
  const name = optionalString(params, 'name')
  if (name === undefined || name.trim() === '') {
    throw invalidParams([{ path: 'name', message: 'a server name is required' }])
  }
  const root = await rootParam(params)
  const trust = optionalBoolean(params, 'trust')
  if (trust !== undefined) {
    if (root === undefined) {
      throw invalidParams([
        { path: 'repoRoot', message: 'trust is about a repository: name its root' },
      ])
    }
    const shown = optionalString(params, 'fingerprint')
    if (trust && (shown === undefined || shown === '')) {
      throw invalidParams([
        { path: 'fingerprint', message: 'trust names the config it was shown: its fingerprint' },
      ])
    }
    const outcome = await servers.trustServer(name, root, trust, shown)
    if (outcome === 'missing') {
      throw invalidParams([
        { path: 'name', message: `${root}/.factory/mcp.json has no server ${name}` },
      ])
    }
    if (outcome === 'changed') {
      throw invalidParams([
        { path: 'fingerprint', message: `${name} changed since it was shown: list it again` },
      ])
    }
    return servers.list(root)
  }

  const path = join(userDir, 'mcp.json')
  const existing = readConfig(path).entries[name]
  const remove = optionalBoolean(params, 'remove') === true
  const config = optionalObject(params, 'server')
  const enabled = optionalBoolean(params, 'enabled')
  if (remove) {
    if (existing === undefined)
      throw invalidParams([{ path: 'name', message: `no global server ${name}` }])
    writeEntry(path, name, undefined)
  } else if (config !== undefined || enabled !== undefined) {
    if (config === undefined && existing === undefined) {
      throw invalidParams([{ path: 'name', message: `no global server ${name}` }])
    }
    let entry: RawEntry = { ...(existing ?? {}) }
    if (config !== undefined) entry = { ...withoutConnection(entry), ...connectionOf(config) }
    if (enabled !== undefined) {
      delete entry['enabled']
      entry['disabled'] = !enabled
    }
    writeEntry(path, name, entry)
  } else {
    throw invalidParams([
      { path: 'server', message: 'server, enabled, remove or trust is required' },
    ])
  }
  await servers.refreshGlobal()
  return servers.list(root)
}

/**
 * `mcp.authorize {name, repoRoot?}`: the page that signs the person in to an HTTP server: the global one, or with
 * `repoRoot` that repository's, which has to be trusted first.
 */
export async function mcpAuthorize(
  servers: McpServers,
  auth: McpAuth,
  params: Record<string, unknown>,
): Promise<unknown> {
  const name = requiredName(params)
  const root = await rootParam(params)
  const found = await servers.authorizable(name, root)
  if (!found.ok) {
    const message = {
      missing: `no server ${name}`,
      'not-http': `${name} is not an HTTP server`,
      untrusted: `${name} runs only once it is allowed`,
    }[found.reason]
    throw invalidParams([{ path: 'name', message }])
  }
  return auth.authorize(found.key, name, found.url)
}

/** `mcp.authorize.cancel {name, repoRoot?}`: stop waiting for that server's sign-in. */
export async function mcpAuthorizeCancel(
  servers: McpServers,
  auth: McpAuth,
  params: Record<string, unknown>,
): Promise<unknown> {
  const name = requiredName(params)
  const root = await rootParam(params)
  auth.cancel(servers.keyOf(name, root))
  return {}
}

function requiredName(params: Record<string, unknown>): string {
  const name = optionalString(params, 'name')
  if (name === undefined || name === '')
    throw invalidParams([{ path: 'name', message: 'a server name is required' }])
  return name
}

function connectionOf(config: Record<string, unknown>): RawEntry {
  const transport = config['transport'] ?? (config['url'] !== undefined ? 'http' : 'stdio')
  if (transport === 'http') {
    const url = config['url']
    if (typeof url !== 'string' || !/^https?:\/\//.test(url)) {
      throw invalidParams([{ path: 'server.url', message: 'an http(s) URL is required' }])
    }
    return {
      type: 'http',
      url,
      ...(isStringMap(config['headers']) ? { headers: config['headers'] } : {}),
    }
  }
  if (transport !== 'stdio') {
    throw invalidParams([{ path: 'server.transport', message: 'stdio or http' }])
  }
  const command = config['command']
  if (typeof command !== 'string' || command.trim() === '') {
    throw invalidParams([{ path: 'server.command', message: 'a command is required' }])
  }
  const args = config['args']
  if (args !== undefined && (!Array.isArray(args) || args.some((arg) => typeof arg !== 'string'))) {
    throw invalidParams([{ path: 'server.args', message: 'a list of strings' }])
  }
  return {
    type: 'stdio',
    command,
    ...(args === undefined ? {} : { args }),
    ...(isStringMap(config['env']) ? { env: config['env'] } : {}),
  }
}

function isStringMap(value: unknown): value is Record<string, string> {
  return (
    typeof value === 'object' &&
    value !== null &&
    !Array.isArray(value) &&
    Object.values(value).every((item) => typeof item === 'string')
  )
}

/** `repoRoot`, which must be a repository's root as the kernel works it out. */
async function rootParam(params: Record<string, unknown>): Promise<string | undefined> {
  const given = optionalString(params, 'repoRoot')
  if (given === undefined || given === '') return undefined
  if (!isAbsolute(given)) throw invalidParams([{ path: 'repoRoot', message: 'an absolute path' }])
  const root = await repoRootOf(given)
  if (root === undefined || root !== resolve(given)) {
    throw invalidParams([
      { path: 'repoRoot', message: `not a repository root (its root is ${root ?? 'unknown'})` },
    ])
  }
  return root
}

/**
 * Before a conversation's turn: read its repository's file, and select the servers it may use — the global
 * ones and its repository's own, never another repository's. The selection is stored as an edit of the
 * default (\`{add}\`), so a conversation whose extensions a client chose outright is left as it was.
 */
export async function selectServers(
  servers: McpServers,
  harness: Harness,
  conversation: Conversation,
  context: Context,
): Promise<void> {
  const agent = await conversation.agent(context)
  const root = agent.cwd === undefined ? undefined : await repoRootOf(agent.cwd)
  await servers.refreshGlobal()
  if (root !== undefined) await servers.refreshRepo(root)
  const wanted = servers.selectionFor(root)
  const stored = (await harness.snapshot(AgentDoc, conversation.id, context))?.extensions
  if (Array.isArray(stored)) return
  const added = stored?.add ?? []
  const others = added.filter((name) => !name.startsWith('mcp-'))
  const had = added.filter((name) => name.startsWith('mcp-'))
  if (had.length === wanted.length && had.every((name, index) => name === wanted[index])) return
  const named = (names: readonly string[]) => names.map((name) => ({ name }) as Extension)
  await conversation.configure(
    {
      extensions: {
        add: named([...others, ...wanted]),
        ...(stored?.remove === undefined ? {} : { remove: named(stored.remove) }),
      },
    },
    context,
  )
}
