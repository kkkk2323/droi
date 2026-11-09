import { describe, expect, test } from 'bun:test'
import { defineTool } from '@earendil-works/pi-durable'
import type { PromptSection, ToolRegistration } from '@earendil-works/pi-durable'
import { createCodemodeExtension } from '@earendil-works/pi-durable/codemode'
import { obj, str } from '@earendil-works/pi-schema'
import { catalogSection } from '../src/extensions/codemode.ts'
import { codingTools } from '../src/extensions/coding.ts'
import { codemodeSandbox } from '../src/codemode/sandbox.ts'

/** The `codemode` section lists every tool of Droi's own, however many MCP servers' tools there are. */

/** A server's tool, with a description long enough that a few fill Pi's budget. */
function serverTool(server: string, n: number): ToolRegistration {
  return defineTool({
    name: `${server}___tool_${n}`,
    description: `Tool ${n} of ${server}. ${'It does a thing and says so at length. '.repeat(20)}`,
    parameters: obj({ query: str({ description: 'What to look for' }) }),
    execute: async () => ({ output: [{ type: 'text' as const, text: '' }] }),
  }) as ToolRegistration
}

async function render(callable: ToolRegistration[]): Promise<string> {
  const namespaces = ['anlan', 'cua', 'exa'].map((name) => ({ name, prefix: `${name}___` }))
  const sandbox = codemodeSandbox()
  const pi = createCodemodeExtension({ sandbox, namespaces, models: false }).sections!.find(
    (s) => s.key === 'codemode',
  )!
  const section = catalogSection(pi as PromptSection, { sandbox, namespaces })
  const input = { agent: { callable } } as unknown as Parameters<PromptSection['render']>[0]
  return (await section.render(input, undefined as never)) ?? ''
}

describe('codemode catalog', () => {
  test("Droi's own tools are listed whole beside three servers' tools; the servers share Pi's budget", async () => {
    const own = codingTools().extension.tools as ToolRegistration[]
    const servers = ['anlan', 'cua', 'exa'].flatMap((server) =>
      Array.from({ length: 12 }, (_, n) => serverTool(server, n)),
    )
    const text = await render([...own, ...servers])
    for (const name of ['read', 'write', 'edit', 'bash', 'grep', 'find', 'ls'])
      expect(text).toContain(`### \`${name}\``)
    expect(text).not.toContain('Some tools are not listed')
    for (const server of ['anlan', 'cua', 'exa'])
      expect(text).toContain(`## ${server} (some tools not listed)`)
    expect(text.match(/Tools scripts can call/g)).toHaveLength(1)
  })

  test('without servers the section is the own tools alone', async () => {
    const text = await render(codingTools().extension.tools as ToolRegistration[])
    expect(text.startsWith('Tools scripts can call')).toBe(true)
    expect(text).toContain('### `grep`')
  })

  test("a script is told it gets 1 MiB of a command's output, not the model's 50KB tail", () => {
    const bash = (codingTools().extension.tools as ToolRegistration[]).find(
      (t) => t.name === 'bash',
    )!
    expect(bash.description).toContain('up to 1 MiB')
    expect(bash.description).toContain('do not pipe it into')
    expect(bash.description).not.toContain('If truncated, full output is saved')
  })
})
