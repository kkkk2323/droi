/**
 * A stdio MCP server for the tests: `echo` (read-only) and `touch`. With MARKER set it appends its pid to that
 * file when it starts, so a test can tell whether it ran and whether it is still running.
 */
import { appendFileSync, writeFileSync } from 'node:fs'

const marker = process.env['MARKER']
if (marker !== undefined && marker !== '') appendFileSync(marker, `${process.pid}\n`)

const tools = [
  {
    name: 'echo',
    description: 'Say the text back.',
    inputSchema: { type: 'object', properties: { text: { type: 'string' } }, required: ['text'] },
    annotations: { readOnlyHint: true },
  },
  {
    name: 'touch',
    description: 'Create a file.',
    inputSchema: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] },
  },
]

function answer(id: unknown, result: unknown): void {
  process.stdout.write(`${JSON.stringify({ jsonrpc: '2.0', id, result })}\n`)
}

function handle(message: {
  id?: unknown
  method?: string
  params?: Record<string, unknown>
}): void {
  if (message.id === undefined || message.method === undefined) return
  switch (message.method) {
    case 'initialize':
      answer(message.id, {
        protocolVersion: message.params?.['protocolVersion'] ?? '2025-06-18',
        capabilities: { tools: {} },
        serverInfo: { name: 'fake', version: '1.0.0' },
        ...(process.env['INSTRUCTIONS'] === undefined
          ? {}
          : { instructions: process.env['INSTRUCTIONS'] }),
      })
      return
    case 'ping':
      answer(message.id, {})
      return
    case 'tools/list':
      answer(message.id, { tools })
      return
    case 'tools/call': {
      const name = message.params?.['name']
      const args = (message.params?.['arguments'] ?? {}) as Record<string, unknown>
      if (name === 'echo') {
        answer(message.id, { content: [{ type: 'text', text: `echo: ${String(args['text'])}` }] })
        return
      }
      if (name === 'touch') {
        writeFileSync(String(args['path']), 'touched')
        answer(message.id, { content: [{ type: 'text', text: 'touched' }] })
        return
      }
      answer(message.id, {
        content: [{ type: 'text', text: `no tool ${String(name)}` }],
        isError: true,
      })
      return
    }
    default:
      process.stdout.write(
        `${JSON.stringify({ jsonrpc: '2.0', id: message.id, error: { code: -32601, message: 'method not found' } })}\n`,
      )
  }
}

let buffer = ''
process.stdin.setEncoding('utf8')
process.stdin.on('data', (chunk: string) => {
  buffer += chunk
  for (;;) {
    const end = buffer.indexOf('\n')
    if (end < 0) break
    const line = buffer.slice(0, end).trim()
    buffer = buffer.slice(end + 1)
    if (line === '') continue
    try {
      handle(JSON.parse(line) as never)
    } catch {
      // Not JSON: ignored, as a server would.
    }
  }
})
process.stdin.on('end', () => process.exit(0))
