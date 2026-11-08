// A stand-in for a Relay in front of the Fake Kernel's Gateway: it answers under /c/<Relay Id>/ as a
// Relay addresses a computer (ADR 0034), or at its root as the computer's network address
// would, and passes everything on, WebSockets included. The Relay's own protocol, between it
// and the Host, is the Go tests' (apps/native/internal/relay); the phone sees only this.
import { createServer, request } from 'node:http'
import type { AddressInfo, Socket } from 'node:net'
import { connect } from 'node:net'

export const RELAY_ID = 'abcdefghijklmnopqrstuvwx'

export interface RelayStandIn {
  /** The address the phone is given: the origin, with /c/<Relay Id> for a Relay. */
  base: string
  /** The paths it passed on, the Gateway's own (/kernel?token=..., /meta). */
  requests: string[]
  /** Stops answering and drops every connection, as a network the phone left. */
  close(): Promise<void>
}

export async function startRelayStandIn(
  target: string,
  {
    prefix = `/c/${RELAY_ID}`,
    relayInMeta,
  }: {
    prefix?: string
    /**
     * The Relay the Gateway's /meta names through this stand-in, as a computer with one does. A Relay
     * stand-in names itself unless told otherwise: the Gateway behind it has no Relay of its own, and a /meta
     * naming none would have the phone forget it.
     */
    relayInMeta?: () => string
  } = {},
): Promise<RelayStandIn> {
  const t = new URL(target)
  let base = ''
  const named = relayInMeta ?? (prefix ? () => base : undefined)
  const requests: string[] = []
  const sockets = new Set<Socket>()
  const rewrite = (url = ''): string | null => {
    if (!prefix) return url
    if (url !== prefix && !url.startsWith(`${prefix}/`) && !url.startsWith(`${prefix}?`))
      return null
    const rest = url.slice(prefix.length)
    return rest.startsWith('/') ? rest : `/${rest}`
  }
  const server = createServer((req, res) => {
    const path = rewrite(req.url)
    if (path === null) {
      res.writeHead(404, { 'access-control-allow-origin': '*' }).end()
      return
    }
    requests.push(path)
    const up = request(
      {
        host: t.hostname,
        port: t.port,
        method: req.method,
        path,
        headers: { ...req.headers, host: t.host },
      },
      (answer) => {
        if (named && path === '/meta') {
          const chunks: Buffer[] = []
          answer.on('data', (chunk: Buffer) => chunks.push(chunk))
          answer.on('end', () => {
            const meta = JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown>
            const body = JSON.stringify({ ...meta, relay: named() })
            const { 'content-length': _length, ...headers } = answer.headers
            res.writeHead(answer.statusCode ?? 502, headers).end(body)
          })
          return
        }
        res.writeHead(answer.statusCode ?? 502, answer.headers)
        answer.pipe(res)
      },
    )
    up.on('error', () => {
      if (!res.headersSent) res.writeHead(502, { 'access-control-allow-origin': '*' })
      res.end()
    })
    req.pipe(up)
  })
  server.on('connection', (socket) => {
    sockets.add(socket)
    socket.on('close', () => sockets.delete(socket))
  })
  server.on('upgrade', (req, socket, head) => {
    const path = rewrite(req.url)
    if (path === null) {
      socket.destroy()
      return
    }
    requests.push(path)
    const up = connect(Number(t.port), t.hostname, () => {
      const lines = [`${req.method} ${path} HTTP/1.1`]
      for (let i = 0; i < req.rawHeaders.length; i += 2) {
        const name = req.rawHeaders[i] ?? ''
        lines.push(
          `${name}: ${name.toLowerCase() === 'host' ? t.host : (req.rawHeaders[i + 1] ?? '')}`,
        )
      }
      up.write(`${lines.join('\r\n')}\r\n\r\n`)
      if (head.length > 0) up.write(head)
      up.pipe(socket)
      socket.pipe(up)
    })
    sockets.add(up)
    up.on('close', () => sockets.delete(up))
    up.on('error', () => socket.destroy())
    socket.on('error', () => up.destroy())
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address() as AddressInfo
  base = `http://127.0.0.1:${port}${prefix}`
  return {
    base,
    requests,
    close: () =>
      new Promise<void>((resolve) => {
        for (const socket of sockets) socket.destroy()
        server.close(() => resolve())
      }),
  }
}
