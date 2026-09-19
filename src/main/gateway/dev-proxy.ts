import { request as httpRequest, type IncomingMessage, type ServerResponse } from 'node:http'
import { connect } from 'node:net'
import type { Duplex } from 'node:stream'

// In development the Client is served by the vite dev server. The Gateway
// still fronts it so the Local Client loads from one origin in both modes;
// these two helpers forward plain requests and vite's HMR upgrade to vite.

export function proxyHttp(target: URL, request: IncomingMessage, response: ServerResponse): void {
  const upstream = httpRequest(
    {
      host: target.hostname,
      port: target.port,
      method: request.method,
      path: request.url,
      headers: { ...request.headers, host: target.host },
    },
    (upstreamResponse) => {
      response.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers)
      upstreamResponse.pipe(response)
    },
  )
  upstream.on('error', () => {
    response.writeHead(502)
    response.end('Client dev server unreachable')
  })
  request.pipe(upstream)
}

export function proxyUpgrade(
  target: URL,
  request: IncomingMessage,
  socket: Duplex,
  head: Buffer,
): void {
  const upstream = connect(Number(target.port), target.hostname, () => {
    const headers = Object.entries({ ...request.headers, host: target.host })
      .map(([name, value]) => `${name}: ${Array.isArray(value) ? value.join(', ') : value}`)
      .join('\r\n')
    upstream.write(`${request.method} ${request.url} HTTP/1.1\r\n${headers}\r\n\r\n`)
    if (head.length) upstream.write(head)
    upstream.pipe(socket)
    socket.pipe(upstream)
  })
  const closeBoth = () => {
    socket.destroy()
    upstream.destroy()
  }
  upstream.on('error', closeBoth)
  socket.on('error', closeBoth)
}
