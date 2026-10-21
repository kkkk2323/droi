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
// The scenario is the JSON serve.ts reads from DROID_FAKE_SCENARIO. The link is
// the pairing link the Desktop Shell shows for that Fake Daemon.
import { spawn } from 'node:child_process'
import { createServer } from 'node:http'
import { createInterface } from 'node:readline'

const port = Number(process.env.DROI_HOST_PORT ?? 41999)
const daemonScript = process.env.FAKE_DAEMON_MJS
if (!daemonScript) throw new Error('FAKE_DAEMON_MJS is not set')

/** @type {Map<string, {child: import('node:child_process').ChildProcess, pending: Map<number, (answer: any) => void>, nextCommand: number}>} */
const daemons = new Map()
let nextId = 1

function start({ scenario, appVersion }) {
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
        const url = 'http://' + ws.host
        resolve({ id, url, link: url + '/#pair=' + token, token, sessions: message.sessions })
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
  for (const { child } of daemons.values()) child.kill()
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
    if (path[0] === 'stop') {
      const entry = daemons.get(path[1])
      if (entry) {
        entry.child.kill()
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
