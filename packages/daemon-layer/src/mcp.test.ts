import { describe, expect, test } from 'vitest'
import {
  EMPTY_SERVER_FORM,
  canRemove,
  formFromRegistry,
  isReadOnly,
  needsSignIn,
  parseServerForm,
  sortServers,
  type McpRegistryEntry,
  type McpServer,
} from './mcp'

const server = (overrides: Partial<McpServer>): McpServer =>
  ({
    name: 'files',
    status: 'connected',
    source: 'user',
    isManaged: false,
    serverType: 'stdio',
    ...overrides,
  }) as McpServer

describe('parseServerForm', () => {
  test('a stdio server needs a name and a command; args split on whitespace', () => {
    expect(parseServerForm({ ...EMPTY_SERVER_FORM, command: 'npx x' })).toMatchObject({
      ok: false,
    })
    expect(parseServerForm({ ...EMPTY_SERVER_FORM, name: 'bad name', command: 'x' })).toMatchObject(
      { ok: false },
    )
    expect(
      parseServerForm({ ...EMPTY_SERVER_FORM, name: 'fs', command: 'npx', args: ' -y  server ' }),
    ).toEqual({
      ok: true,
      params: { name: 'fs', type: 'stdio', command: 'npx', args: ['-y', 'server'] },
    })
  })

  test('an http server needs a URL; headers are "Name: value" lines', () => {
    expect(
      parseServerForm({ ...EMPTY_SERVER_FORM, name: 'api', type: 'http', url: 'localhost' }),
    ).toMatchObject({ ok: false })
    expect(
      parseServerForm({
        ...EMPTY_SERVER_FORM,
        name: 'api',
        type: 'http',
        url: 'https://mcp.example.com/mcp',
        headers: 'Authorization: Bearer x\n\nX-Team: a: b',
      }),
    ).toEqual({
      ok: true,
      params: {
        name: 'api',
        type: 'http',
        url: 'https://mcp.example.com/mcp',
        headers: { Authorization: 'Bearer x', 'X-Team': 'a: b' },
      },
    })
    expect(
      parseServerForm({
        ...EMPTY_SERVER_FORM,
        name: 'api',
        type: 'sse',
        url: 'https://x.y/sse',
        headers: 'nonsense',
      }),
    ).toMatchObject({ ok: false })
  })
})

test('a registry entry fills the form', () => {
  const entry = {
    name: 'github',
    description: 'GitHub',
    type: 'http',
    url: 'https://api.githubcopilot.com/mcp/',
  } as McpRegistryEntry
  expect(formFromRegistry(entry)).toMatchObject({
    name: 'github',
    type: 'http',
    url: 'https://api.githubcopilot.com/mcp/',
    command: '',
  })
})

test('the organization’s servers are read-only and last; only the user’s can be removed', () => {
  const org = server({ name: 'a-org', source: 'org' } as Partial<McpServer>)
  const project = server({ name: 'z-proj', source: 'project' } as Partial<McpServer>)
  const mine = server({ name: 'm-mine' })
  expect(sortServers([org, project, mine]).map((s) => s.name)).toEqual([
    'm-mine',
    'z-proj',
    'a-org',
  ])
  expect(isReadOnly(org)).toBe(true)
  expect(canRemove(project)).toBe(false)
  expect(canRemove(mine)).toBe(true)
})

test('a server wants signing in when it needs auth and has no tokens', () => {
  expect(needsSignIn(server({ requiresAuth: true }))).toBe(true)
  expect(needsSignIn(server({ requiresAuth: true, hasAuthTokens: true }))).toBe(false)
  expect(needsSignIn(server({}))).toBe(false)
})
