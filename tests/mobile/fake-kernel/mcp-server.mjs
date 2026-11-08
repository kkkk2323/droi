// A stdio MCP server for the Fake Kernel's Scenarios: its tools are the names in TOOLS (comma separated),
// each answering with what it was given. Plain JavaScript, so the Kernel starts it with `node` as it would any
// server in a person's mcp.json.
const tools = (process.env.TOOLS || 'read_file')
  .split(',')
  .filter(Boolean)
  .map((name) => ({
    name,
    description: `The ${name} tool.`,
    inputSchema: { type: 'object', properties: { text: { type: 'string' } } },
  }))

function send(message) {
  process.stdout.write(`${JSON.stringify({ jsonrpc: '2.0', ...message })}\n`)
}

function handle(message) {
  if (message.id === undefined || message.method === undefined) return
  switch (message.method) {
    case 'initialize':
      send({
        id: message.id,
        result: {
          protocolVersion: message.params?.protocolVersion ?? '2025-06-18',
          capabilities: { tools: {} },
          serverInfo: { name: 'fake', version: '1.0.0' },
        },
      })
      return
    case 'ping':
      send({ id: message.id, result: {} })
      return
    case 'tools/list':
      send({ id: message.id, result: { tools } })
      return
    case 'tools/call':
      send({
        id: message.id,
        result: {
          content: [
            {
              type: 'text',
              text: `${message.params?.name}: ${JSON.stringify(message.params?.arguments ?? {})}`,
            },
          ],
        },
      })
      return
    default:
      send({ id: message.id, error: { code: -32601, message: 'method not found' } })
  }
}

let buffer = ''
process.stdin.setEncoding('utf8')
process.stdin.on('data', (chunk) => {
  buffer += chunk
  for (;;) {
    const end = buffer.indexOf('\n')
    if (end < 0) break
    const line = buffer.slice(0, end).trim()
    buffer = buffer.slice(end + 1)
    if (!line) continue
    try {
      handle(JSON.parse(line))
    } catch {
      // Not JSON: ignored, as a server would.
    }
  }
})
process.stdin.on('end', () => process.exit(0))
