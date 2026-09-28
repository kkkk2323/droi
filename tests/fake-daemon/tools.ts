// The skills and MCP Servers a Fake Daemon reports and lets a Client switch,
// add and remove: one set per Fake Daemon, like the settings and mcp.json
// behind the real one. Changes push `mcp_status_changed` to every connection
// as the Daemon does when a connection comes or goes.
import { RpcError, type FakeDaemon } from './fake-daemon'
import type { MethodHandler } from './scenario'

export interface SkillFixture {
  name: string
  description?: string
  /** Defaults to personal. */
  location?: 'project' | 'personal' | 'builtin' | 'automation'
  userInvocable?: boolean
  /** Levels that turned it off; `org` makes it read-only. */
  disabledAt?: Array<'user' | 'project' | 'org'>
  content?: string
}

export interface McpServerFixture {
  name: string
  /** Defaults to connected. */
  status?: 'connecting' | 'connected' | 'disconnected' | 'failed' | 'disabled'
  /** Defaults to user. */
  source?: 'user' | 'project' | 'org'
  type?: 'stdio' | 'http' | 'sse'
  toolCount?: number
  error?: string
  requiresAuth?: boolean
  hasAuthTokens?: boolean
  tools?: Array<{ name: string; description?: string; enabled?: boolean }>
}

export interface McpRegistryFixture {
  name: string
  description: string
  type: 'stdio' | 'http' | 'sse'
  command?: string
  args?: string[]
  url?: string
}

export interface ToolsInput {
  skills?: SkillFixture[]
  /** Whether the Sessions' Workspaces take project-level skill switches. */
  projectAvailable?: boolean
  mcpServers?: McpServerFixture[]
  mcpRegistry?: McpRegistryFixture[]
}

interface ServerState {
  fixture: McpServerFixture
  disabledByUser: boolean
  authenticated: boolean
  pendingAuth: { url: string; state: string } | null
  tools: Array<{ name: string; description?: string; enabled: boolean }>
}

export function toolHandlers(input: ToolsInput): Record<string, MethodHandler> {
  const skills = structuredClone(input.skills ?? []).map((s) => ({
    ...s,
    disabledAt: new Set(s.disabledAt ?? []),
  }))
  const servers: ServerState[] = structuredClone(input.mcpServers ?? []).map((fixture) => ({
    fixture,
    disabledByUser: fixture.status === 'disabled',
    authenticated: fixture.hasAuthTokens === true,
    pendingAuth: null,
    tools: (fixture.tools ?? []).map((t) => ({ ...t, enabled: t.enabled ?? true })),
  }))

  const listSkills = () => ({
    skills: skills.map((skill) => {
      const disabledAt = [...skill.disabledAt]
      return {
        name: skill.name,
        description: skill.description,
        location: skill.location ?? 'personal',
        filePath: `/Users/dev/.factory/skills/${skill.name}/SKILL.md`,
        userInvocable: skill.userInvocable ?? true,
        enabled: disabledAt.length === 0,
        ...(skill.content ? { content: skill.content } : {}),
        ...(disabledAt.length > 0
          ? { disabledBy: { kind: 'ledger', sources: disabledAt.map((level) => ({ level })) } }
          : {}),
      }
    }),
    projectAvailable: input.projectAvailable ?? true,
  })

  const statusOf = (server: ServerState) => {
    if (server.disabledByUser) return 'disabled'
    if (server.fixture.requiresAuth && !server.authenticated) return 'failed'
    return server.fixture.status ?? 'connected'
  }
  const listServers = () => {
    const listed = servers.map((server) => ({
      name: server.fixture.name,
      status: statusOf(server),
      source: server.fixture.source ?? 'user',
      isManaged: server.fixture.source === 'org',
      serverType: server.fixture.type ?? 'stdio',
      ...(server.fixture.toolCount !== undefined || server.tools.length
        ? { toolCount: server.fixture.toolCount ?? server.tools.length }
        : {}),
      ...(server.fixture.error ? { error: server.fixture.error } : {}),
      ...(server.fixture.requiresAuth
        ? { requiresAuth: true, hasAuthTokens: server.authenticated }
        : {}),
      ...(server.pendingAuth
        ? {
            pendingAuthUrl: server.pendingAuth.url,
            pendingAuthState: server.pendingAuth.state,
            pendingAuthMessage: `Sign in to ${server.fixture.name} in your browser`,
          }
        : {}),
    }))
    const count = (status: string) => listed.filter((s) => s.status === status).length
    return {
      servers: listed,
      summary: {
        total: listed.length,
        connected: count('connected'),
        connecting: count('connecting'),
        failed: count('failed'),
        disabled: count('disabled'),
      },
    }
  }
  const mustServer = (name: unknown) => {
    const found = servers.find((s) => s.fixture.name === name)
    if (!found) throw new RpcError(-32602, `Unknown MCP server: ${String(name)}`)
    if (found.fixture.source === 'org') {
      throw new RpcError(-32000, 'Org MCP servers cannot be modified')
    }
    return found
  }
  const changed = (daemon: FakeDaemon, sessionId: unknown) =>
    daemon.notify(String(sessionId), { type: 'mcp_status_changed', ...listServers() })

  return {
    'daemon.list_skills': listSkills,
    'daemon.set_skill_disabled': (params) => {
      const found = skills.find((s) => s.name === params['skillName'])
      if (!found) throw new RpcError(-32602, `Unknown skill: ${String(params['skillName'])}`)
      const level = params['settingsLevel'] === 'project' ? 'project' : 'user'
      if (params['disabled'] === true) found.disabledAt.add(level)
      else found.disabledAt.delete(level)
      return { success: true }
    },
    'daemon.list_mcp_servers': listServers,
    'daemon.toggle_mcp_server': (params, context) => {
      const server = mustServer(params['serverName'])
      server.disabledByUser = params['enabled'] !== true
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
    'daemon.remove_mcp_server': (params, context) => {
      const server = mustServer(params['serverName'])
      if ((server.fixture.source ?? 'user') !== 'user') {
        throw new RpcError(-32000, 'Only servers in ~/.factory/mcp.json can be removed')
      }
      servers.splice(servers.indexOf(server), 1)
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
    'daemon.add_mcp_server': (params, context) => {
      const name = String(params['name'])
      if (servers.some((s) => s.fixture.name === name)) {
        throw new RpcError(-32000, `MCP server "${name}" already exists`)
      }
      servers.push({
        fixture: { name, type: params['type'] as McpServerFixture['type'], status: 'connected' },
        disabledByUser: false,
        authenticated: true,
        pendingAuth: null,
        tools: [],
      })
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
    'daemon.list_mcp_registry': () => ({ servers: input.mcpRegistry ?? [] }),
    'daemon.list_mcp_tools': () => ({
      tools: servers.flatMap((server) =>
        server.tools.map((tool) => ({
          serverName: server.fixture.name,
          name: tool.name,
          description: tool.description,
          isEnabled: tool.enabled,
        })),
      ),
    }),
    'daemon.toggle_mcp_tool': (params) => {
      const server = mustServer(params['serverName'])
      const tool = server.tools.find((t) => t.name === params['toolName'])
      if (!tool) throw new RpcError(-32602, `Unknown tool: ${String(params['toolName'])}`)
      tool.enabled = params['enabled'] === true
      return { success: true }
    },
    // The real Daemon opens nothing itself: it reports the page to sign in on
    // and finishes when its callback on the computer gets the code.
    'daemon.authenticate_mcp_server': (params, context) => {
      const server = mustServer(params['serverName'])
      const state = `state-${server.fixture.name}`
      server.pendingAuth = {
        url: `https://auth.example.com/authorize?server=${server.fixture.name}&state=${state}`,
        state,
      }
      context.daemon.notify(String(params['sessionId']), {
        type: 'mcp_auth_required',
        serverName: server.fixture.name,
        authUrl: server.pendingAuth.url,
        message: `Sign in to ${server.fixture.name} in your browser`,
        state,
      })
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
    'daemon.cancel_mcp_auth': (params, context) => {
      const server = mustServer(params['serverName'])
      server.pendingAuth = null
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
    'daemon.clear_mcp_auth': (params, context) => {
      const server = mustServer(params['serverName'])
      server.authenticated = false
      server.pendingAuth = null
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
    // A test stands in for the callback: the code arrives, the server connects.
    'daemon.submit_mcp_auth_code': (params, context) => {
      const server = mustServer(params['serverName'])
      server.authenticated = true
      server.pendingAuth = null
      context.daemon.notify(String(params['sessionId']), {
        type: 'mcp_auth_completed',
        serverName: server.fixture.name,
        outcome: 'success',
        message: 'Signed in',
      })
      changed(context.daemon, params['sessionId'])
      return { success: true }
    },
  }
}
