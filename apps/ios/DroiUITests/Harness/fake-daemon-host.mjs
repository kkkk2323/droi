#!/usr/bin/env node
// Runs on the Mac, beside the simulator: an XCUITest cannot spawn processes, so
// the tests ask this host to start a Fake Daemon (the bundled fake-daemon.mjs)
// with a scenario, talk to it over its stdin, and stop it. Plain node, no packages.
//
//   POST /start        {scenario, appVersion?}  ->  {id, link, url, sessions}
//   POST /command/<id> {op, ...}                ->  the Fake Daemon's answer
//   POST /stop/<id>                             ->  {}
//   POST /stop-all                              ->  {}
//   GET  /health                                ->  {}
//
//   POST /gateway/<id> {op, ...}                ->  changes the Gateway in front of it
//
// The scenario is the JSON serve.ts reads from DROID_FAKE_SCENARIO. The link is
// the pairing link the Desktop Shell shows for that Fake Daemon.
//
// Each Fake Daemon sits behind a small Gateway of the host's own (a forwarding
// proxy), because the Fake Daemon's command line cannot do what a Desktop
// Shell does to its Gateway. Its ops:
//   reset                a new Pairing Token; the old one is refused. Answers {link}
//   revoke               every Pairing Token is refused
//   meta {name?, version?}  what /meta says from now on
//   requests             the Gateway's HTTP requests so far: [{method, path, query}]
//   dropSockets          closes the open WebSockets
import { spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { createServer, request as httpRequest } from 'node:http'
import { connect } from 'node:net'
import { createInterface } from 'node:readline'

const port = Number(process.env.DROI_HOST_PORT ?? 41999)
const daemonScript = process.env.FAKE_DAEMON_MJS
if (!daemonScript) throw new Error('FAKE_DAEMON_MJS is not set')

/** @type {Map<string, {child: import('node:child_process').ChildProcess, pending: Map<number, (answer: any) => void>, nextCommand: number, gateway?: any, url?: string}>} */
const daemons = new Map()
let nextId = 1

/** A forwarding Gateway: checks the Pairing Token itself and can rewrite /meta. */
function startGateway(daemonPort, realToken, meta) {
  const state = { accepted: realToken, meta: { ...meta }, log: [], sockets: new Set() }
  const forwarded = (url) => {
    url.searchParams.has('token') && url.searchParams.set('token', realToken)
    return url.pathname + url.search
  }
  const refused = (url) => url.searchParams.has('token') && url.searchParams.get('token') !== state.accepted
  const server = createServer((request, response) => {
    const url = new URL(request.url, 'http://gateway')
    if (url.pathname !== '/meta') {
      const query = Object.fromEntries([...url.searchParams].filter(([key]) => key !== 'token'))
      state.log.push({ method: request.method, path: url.pathname, query })
    }
    if (refused(url)) {
      response.writeHead(401, { 'access-control-allow-origin': '*' })
      return response.end()
    }
    const upstream = httpRequest(
      { host: '127.0.0.1', port: daemonPort, path: forwarded(url), method: request.method, headers: request.headers },
      (answer) => {
        if (url.pathname !== '/meta') {
          response.writeHead(answer.statusCode, answer.headers)
          return answer.pipe(response)
        }
        let body = ''
        answer.on('data', (chunk) => (body += chunk))
        answer.on('end', () => {
          const text = JSON.stringify({ ...JSON.parse(body), ...state.meta })
          response.writeHead(answer.statusCode, { ...answer.headers, 'content-length': Buffer.byteLength(text) })
          response.end(text)
        })
      },
    )
    request.pipe(upstream)
  })
  server.on('upgrade', (request, socket, head) => {
    const url = new URL(request.url, 'http://gateway')
    if (refused(url)) {
      socket.end('HTTP/1.1 401 Unauthorized\r\nConnection: close\r\n\r\n')
      return
    }
    const upstream = connect(daemonPort, '127.0.0.1', () => {
      const headers = request.rawHeaders.reduce((text, item, index) => text + item + (index % 2 ? '\r\n' : ': '), '')
      upstream.write(`${request.method} ${forwarded(url)} HTTP/1.1\r\n${headers}\r\n`)
      upstream.write(head)
      socket.pipe(upstream)
      upstream.pipe(socket)
    })
    state.sockets.add(socket)
    for (const end of [socket, upstream]) {
      end.on('close', () => {
        state.sockets.delete(socket)
        socket.destroy()
        upstream.destroy()
      })
      end.on('error', () => {})
    }
  })
  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () => resolve({ server, state, port: server.address().port }))
  })
}

function gatewayCommand(entry, body) {
  const { state } = entry.gateway
  switch (body.op) {
    case 'reset': {
      state.accepted = 'reset-' + randomUUID()
      for (const socket of state.sockets) socket.destroy()
      return { link: entry.url + '/#pair=' + state.accepted }
    }
    case 'revoke':
      state.accepted = null
      for (const socket of state.sockets) socket.destroy()
      return {}
    case 'meta':
      Object.assign(state.meta, body.meta)
      return {}
    case 'requests':
      return { requests: state.log }
    case 'dropSockets':
      for (const socket of state.sockets) socket.destroy()
      return {}
    default:
      throw new Error('unknown gateway op ' + body.op)
  }
}

function start({ scenario, appVersion, name }) {
  const id = String(nextId++)
  const child = spawn('node', [daemonScript], {
    env: {
      ...process.env,
      DROID_FAKE_SCENARIO: JSON.stringify(scenario ?? {}),
      ...(appVersion ? { DROID_FAKE_APP_VERSION: appVersion } : {}),
    },
    stdio: ['pipe', 'pipe', 'inherit'],
  })
  const entry = { child, pending: new Map(), nextCommand: 1 }
  daemons.set(id, entry)
  const lines = createInterface({ input: child.stdout })
  return new Promise((resolve, reject) => {
    let started = false
    child.once('exit', (code) => {
      entry.gateway?.server.close()
      daemons.delete(id)
      if (!started) reject(new Error('Fake Daemon exited with ' + code))
    })
    lines.on('line', (line) => {
      let message
      try {
        message = JSON.parse(line)
      } catch {
        return
      }
      if (!started) {
        started = true
        const ws = new URL(message.url)
        const token = ws.searchParams.get('token')
        startGateway(Number(ws.port), token, name ? { name } : {}).then((gateway) => {
          entry.gateway = gateway
          entry.url = 'http://127.0.0.1:' + gateway.port
          resolve({ id, url: entry.url, link: entry.url + '/#pair=' + token, token, sessions: message.sessions })
        })
        return
      }
      entry.pending.get(message.id)?.(message)
      entry.pending.delete(message.id)
    })
  })
}

function command(id, body) {
  const entry = daemons.get(id)
  if (!entry) return Promise.reject(new Error('no Fake Daemon ' + id))
  const commandId = entry.nextCommand++
  return new Promise((resolve) => {
    entry.pending.set(commandId, resolve)
    entry.child.stdin.write(JSON.stringify({ ...body, id: commandId }) + '\n')
    if (body.op === 'stop') daemons.delete(id)
  })
}

function stopAll() {
  for (const { child, gateway } of daemons.values()) {
    child.kill()
    gateway?.state.sockets.forEach((socket) => socket.destroy())
    gateway?.server.close()
  }
  daemons.clear()
}

async function readBody(request) {
  let text = ''
  for await (const chunk of request) text += chunk
  return text ? JSON.parse(text) : {}
}

const server = createServer(async (request, response) => {
  const reply = (status, body) => {
    response.writeHead(status, { 'content-type': 'application/json' })
    response.end(JSON.stringify(body))
  }
  try {
    const path = new URL(request.url, 'http://host').pathname.split('/').filter(Boolean)
    if (request.method === 'GET' && path[0] === 'health') return reply(200, {})
    if (request.method !== 'POST') return reply(405, { error: 'POST only' })
    if (path[0] === 'start') return reply(200, await start(await readBody(request)))
    if (path[0] === 'command') return reply(200, await command(path[1], await readBody(request)))
    if (path[0] === 'gateway') {
      const entry = daemons.get(path[1])
      if (!entry?.gateway) return reply(404, { error: 'no Fake Daemon ' + path[1] })
      return reply(200, gatewayCommand(entry, await readBody(request)))
    }
    if (path[0] === 'stop') {
      const entry = daemons.get(path[1])
      if (entry) {
        entry.child.kill()
        entry.gateway?.state.sockets.forEach((socket) => socket.destroy())
        entry.gateway?.server.close()
        daemons.delete(path[1])
      }
      return reply(200, {})
    }
    if (path[0] === 'stop-all') {
      stopAll()
      return reply(200, {})
    }
    reply(404, { error: 'unknown path' })
  } catch (error) {
    reply(500, { error: String(error) })
  }
})
server.listen(port, '127.0.0.1', () => console.log('fake-daemon-host listening on ' + port))

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    stopAll()
    process.exit(0)
  })
}
