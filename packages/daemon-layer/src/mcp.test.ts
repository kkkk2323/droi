import { describe, expect, test } from 'vitest'
import {
  EMPTY_SERVER_FORM,
  canRemove,
  isReadOnly,
  needsSignIn,
  needsTrust,
  parseServerForm,
  serverCommand,
  sortServers,
  toolLabel,
  type McpServer,
} from './mcp'

const server = (overrides: Partial<McpServer>): McpServer => ({
  name: 'files',
  status: 'connected',
  enabled: true,
  tools: [],
  error: null,
  transport: 'stdio',
  ...overrides,
})

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
      params: { name: 'fs', transport: 'stdio', command: 'npx', args: ['-y', 'server'] },
    })
  })

  test('an http server needs a URL; headers are "Name: value" lines', () => {
    expect(
      parseServerForm({ ...EMPTY_SERVER_FORM, name: 'api', transport: 'http', url: 'localhost' }),
    ).toMatchObject({ ok: false })
    expect(
      parseServerForm({
        ...EMPTY_SERVER_FORM,
        name: 'api',
        transport: 'http',
        url: 'https://mcp.example.com/mcp',
        headers: 'Authorization: Bearer x\n\nX-Team: a: b',
      }),
    ).toEqual({
      ok: true,
      params: {
        name: 'api',
        transport: 'http',
        url: 'https://mcp.example.com/mcp',
        headers: { Authorization: 'Bearer x', 'X-Team': 'a: b' },
      },
    })
    expect(
      parseServerForm({
        ...EMPTY_SERVER_FORM,
        name: 'api',
        transport: 'http',
        url: 'https://x.y/mcp',
        headers: 'nonsense',
      }),
    ).toMatchObject({ ok: false })
  })
})

test('a repository’s servers and the app’s are read-only and last; only the user’s can be removed', () => {
  const repo = server({ name: 'a-repo', repoRoot: '/w/app', trusted: true })
  const app = server({ name: 'b-app', app: true })
  const mine = server({ name: 'm-mine' })
  expect(sortServers([repo, app, mine]).map((s) => s.name)).toEqual(['m-mine', 'a-repo', 'b-app'])
  expect(isReadOnly(repo)).toBe(true)
  expect(isReadOnly(app)).toBe(true)
  expect(canRemove(repo)).toBe(false)
  expect(canRemove(mine)).toBe(true)
})

test('a server wants signing in when it failed for want of authorization', () => {
  expect(needsSignIn(server({ status: 'failed', error: 'authorization required (401)' }))).toBe(
    true,
  )
  expect(needsSignIn(server({ status: 'failed', error: 'spawn ENOENT' }))).toBe(false)
  expect(needsSignIn(server({}))).toBe(false)
})

test('a repository’s server waits for an OK, and shows what it would run', () => {
  const waiting = server({
    status: 'untrusted',
    repoRoot: '/w/app',
    command: 'npx',
    args: ['-y', 'srv'],
  })
  expect(needsTrust(waiting)).toBe(true)
  expect(serverCommand(waiting)).toBe('npx -y srv')
  expect(serverCommand(server({ transport: 'http', url: 'https://x.y/mcp' }))).toBe(
    'https://x.y/mcp',
  )
})

test('a tool is shown without its server’s prefix', () => {
  expect(toolLabel({ name: 'linear' }, 'linear___create_issue')).toBe('create_issue')
  expect(toolLabel({ name: 'my.srv' }, 'my_srv___x')).toBe('x')
  expect(toolLabel({ name: 'linear' }, 'other')).toBe('other')
})
