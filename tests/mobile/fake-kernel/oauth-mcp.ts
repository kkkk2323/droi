// An HTTP MCP server that asks for OAuth, as apps/kernel/test/mcp-oauth.test.ts has under Bun: discovery,
// dynamic registration, an authorize page that redirects at once with a code, a token endpoint, and /mcp,
// which answers 401 until it is given the token.
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'

const TOKEN = 'good-token'

export interface OAuthMcpServer {
  /** The server's MCP address, for an mcp.json entry's `url`. */
  url: string
  close(): Promise<void>
}

async function body(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = []
  for await (const chunk of req) chunks.push(chunk as Buffer)
  return Buffer.concat(chunks).toString('utf8')
}

export async function startOAuthMcpServer(): Promise<OAuthMcpServer> {
  let base = ''
  const server = createServer((req, res) => void serve(req, res))
  const json = (res: ServerResponse, value: unknown, status = 200) => {
    res.writeHead(status, { 'content-type': 'application/json' })
    res.end(JSON.stringify(value))
  }
  const serve = async (req: IncomingMessage, res: ServerResponse) => {
    const url = new URL(req.url ?? '/', base)
    switch (url.pathname) {
      case '/.well-known/oauth-protected-resource':
      case '/.well-known/oauth-protected-resource/mcp':
        return json(res, { resource: `${base}/mcp`, authorization_servers: [base] })
      case '/.well-known/oauth-authorization-server':
        return json(res, {
          issuer: base,
          authorization_endpoint: `${base}/authorize`,
          token_endpoint: `${base}/token`,
          registration_endpoint: `${base}/register`,
          response_types_supported: ['code'],
          grant_types_supported: ['authorization_code', 'refresh_token'],
          code_challenge_methods_supported: ['S256'],
          token_endpoint_auth_methods_supported: ['none'],
        })
      case '/register':
        return json(
          res,
          { ...(JSON.parse(await body(req)) as object), client_id: 'droi-client' },
          201,
        )
      case '/authorize': {
        const redirect = new URL(url.searchParams.get('redirect_uri')!)
        redirect.searchParams.set('code', 'the-code')
        redirect.searchParams.set('state', url.searchParams.get('state') ?? '')
        res.writeHead(302, { location: redirect.toString() })
        return res.end()
      }
      case '/token': {
        const form = new URLSearchParams(await body(req))
        if (form.get('grant_type') === 'authorization_code' && form.get('code') !== 'the-code') {
          return json(res, { error: 'invalid_grant' }, 400)
        }
        return json(res, {
          access_token: TOKEN,
          token_type: 'Bearer',
          expires_in: 3600,
          refresh_token: 'r1',
        })
      }
      case '/mcp': {
        if (req.headers.authorization !== `Bearer ${TOKEN}`) {
          res.writeHead(401, {
            'www-authenticate': `Bearer resource_metadata="${base}/.well-known/oauth-protected-resource/mcp"`,
          })
          return res.end('unauthorized')
        }
        if (req.method === 'GET') return res.writeHead(405).end()
        if (req.method === 'DELETE') return res.writeHead(200).end()
        const message = JSON.parse(await body(req)) as {
          id?: unknown
          method: string
          params?: Record<string, unknown>
        }
        if (message.id === undefined) return res.writeHead(202).end()
        const answer = (result: unknown) => json(res, { jsonrpc: '2.0', id: message.id, result })
        if (message.method === 'initialize') {
          return answer({
            protocolVersion: message.params?.['protocolVersion'],
            capabilities: { tools: {} },
            serverInfo: { name: 'secure', version: '1' },
          })
        }
        if (message.method === 'tools/list')
          return answer({ tools: [{ name: 'whoami', inputSchema: { type: 'object' } }] })
        return answer({ content: [{ type: 'text', text: 'you' }] })
      }
    }
    res.writeHead(404).end('not found')
  }
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
  return {
    url: `${base}/mcp`,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections()
        server.close(() => resolve())
      }),
  }
}
