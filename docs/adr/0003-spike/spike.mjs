/**
 * Spike for ADR 0003: what happens when one Session is open from two Daemon
 * connections at once.
 *
 * Topology:
 *   client A ─┐
 *   client B ─┼─ logging ws proxy ─ droid daemon (loopback)
 *   client C ─┘
 *
 * The proxy writes every frame in both directions to raw.log tagged with the
 * connection label, so we see what the Daemon actually sends each socket.
 *
 * Steps:
 *   1. A creates a Session (autonomy off so every tool asks permission).
 *   2. B resumes the same Session before any turn starts.
 *   3. A streams a prompt that runs a shell command (permission) and then
 *      asks the user a question (ask_user).
 *   4. C resumes the Session mid-turn.
 *   Both A and B register permission/ask-user handlers that log and answer
 *   with different delays so we can see who gets asked and what happens to the
 *   second answer.
 */
import { spawn } from 'node:child_process';
import { readFileSync, writeFileSync, mkdtempSync, appendFileSync } from 'node:fs';
import { tmpdir, homedir } from 'node:os';
import { join } from 'node:path';
import net from 'node:net';
import { WebSocketServer, WebSocket } from 'ws';
import {
  connectToDaemon,
  AutonomyLevel,
  ToolConfirmationOutcome,
  DroidMessageType,
} from '@factory/droid-sdk';

const apiKey = JSON.parse(readFileSync(join(homedir(), '.droid-app/app-state.json'), 'utf8')).apiKey;
if (!apiKey) throw new Error('no apiKey in ~/.droid-app/app-state.json');

const RAW = join(process.cwd(), 'raw.log');
const EVENTS = join(process.cwd(), 'events.log');
writeFileSync(RAW, '');
writeFileSync(EVENTS, '');
const t0 = Date.now();
const ts = () => String(Date.now() - t0).padStart(6, ' ');
function ev(msg) {
  const line = `[${ts()}ms] ${msg}`;
  console.log(line);
  appendFileSync(EVENTS, line + '\n');
}
function redact(text) {
  return text.replaceAll(apiKey, '<API_KEY>');
}
function summarize(text) {
  try {
    const m = JSON.parse(text);
    const parts = [];
    if (m.method) parts.push(`method=${m.method}`);
    if (m.id !== undefined) parts.push(`id=${m.id}`);
    if (m.params?.notification?.type) parts.push(`notif=${m.params.notification.type}`);
    if (m.params?.notification?.notification?.method) parts.push(`inner=${m.params.notification.notification.method}`);
    if (m.result !== undefined) parts.push('result');
    if (m.error) parts.push(`error=${JSON.stringify(m.error).slice(0, 200)}`);
    return parts.join(' ');
  } catch {
    return text.slice(0, 80);
  }
}

async function freePort() {
  return new Promise((res) => {
    const s = net.createServer();
    s.listen(0, '127.0.0.1', () => {
      const p = s.address().port;
      s.close(() => res(p));
    });
  });
}

const daemonPort = await freePort();
const proxyPort = await freePort();

// The daemon only accepts credentials of the user who owns the computer
// registration in $HOME/.factory. Running it with an isolated HOME and the same
// key the clients present sidesteps a mismatch with the CLI login on this box.
const daemon = spawn('droid', ['daemon', '--port', String(daemonPort), '--parent-pid', String(process.pid)], {
  stdio: ['ignore', 'pipe', 'pipe'],
  env: { ...process.env, HOME: join(process.cwd(), 'home'), FACTORY_API_KEY: apiKey },
});
daemon.stdout.on('data', (d) => appendFileSync(EVENTS, `[daemon out] ${redact(d.toString())}`));
daemon.stderr.on('data', (d) => appendFileSync(EVENTS, `[daemon err] ${redact(d.toString())}`));

await new Promise((r) => setTimeout(r, 1500));
for (let i = 0; i < 40; i++) {
  const ok = await new Promise((res) => {
    const s = net.connect(daemonPort, '127.0.0.1');
    s.once('connect', () => { s.destroy(); res(true); });
    s.once('error', () => res(false));
  });
  if (ok) break;
  await new Promise((r) => setTimeout(r, 250));
}
ev(`daemon listening on ${daemonPort}`);

// Logging proxy. The label is carried in the URL path: ws://.../<label>
const wss = new WebSocketServer({ port: proxyPort, host: '127.0.0.1' });
wss.on('connection', (client, req) => {
  const label = (req.url ?? '/?').slice(1) || '?';
  const upstream = new WebSocket(`ws://127.0.0.1:${daemonPort}`);
  const queue = [];
  upstream.on('open', () => { for (const q of queue) upstream.send(q); queue.length = 0; });
  client.on('message', (data) => {
    const text = data.toString();
    appendFileSync(RAW, `[${ts()}ms] ${label} -> daemon ${redact(text)}\n`);
    ev(`${label} -> daemon ${summarize(text)}`);
    if (upstream.readyState === WebSocket.OPEN) upstream.send(text); else queue.push(text);
  });
  upstream.on('message', (data) => {
    const text = data.toString();
    appendFileSync(RAW, `[${ts()}ms] daemon -> ${label} ${redact(text)}\n`);
    ev(`daemon -> ${label} ${summarize(text)}`);
    if (client.readyState === WebSocket.OPEN) client.send(text);
  });
  client.on('close', () => { ev(`${label} closed`); upstream.close(); });
  upstream.on('close', () => { ev(`daemon closed ${label}`); client.close(); });
  upstream.on('error', (e) => ev(`upstream error ${label}: ${e.message}`));
});
ev(`proxy listening on ${proxyPort}`);

const url = (label) => `ws://127.0.0.1:${proxyPort}/${label}`;
const cwd = mkdtempSync(join(tmpdir(), 'droi-spike-ws-'));
writeFileSync(join(cwd, 'README.md'), '# spike workspace\n');

function handlers(label, permDelayMs, askDelayMs) {
  return {
    permissionHandler: async (params) => {
      ev(`${label} PERMISSION received tools=${params.toolUses.map((t) => t.toolUse.name).join(',')}`);
      await new Promise((r) => setTimeout(r, permDelayMs));
      ev(`${label} PERMISSION answering ProceedOnce`);
      return ToolConfirmationOutcome.ProceedOnce;
    },
    askUserHandler: async (params) => {
      ev(`${label} ASK_USER received questions=${JSON.stringify(params.questions?.map((q) => q.question ?? q.header ?? q)).slice(0, 200)}`);
      await new Promise((r) => setTimeout(r, askDelayMs));
      ev(`${label} ASK_USER answering`);
      return {
        answers: (params.questions ?? []).map((q, index) => ({
          index,
          question: q.question ?? String(index),
          answer: `${label} says blue`,
        })),
      };
    },
  };
}

const A = await connectToDaemon({ url: url('A'), auth: { apiKey }, onError: (e) => ev(`A onError ${e.message}`) });
const B = await connectToDaemon({ url: url('B'), auth: { apiKey }, onError: (e) => ev(`B onError ${e.message}`) });
ev('A and B authenticated');

const sessionA = await A.sessions.create({
  cwd,
  autonomyLevel: AutonomyLevel.Off,
  ...handlers('A', 3000, 3000),
});
ev(`A created session ${sessionA.id}`);

const sessionB = await B.sessions.resume(sessionA.id, handlers('B', 6000, 6000));
ev(`B resumed session ${sessionB.id}`);

let sessionC;
setTimeout(async () => {
  try {
    const C = await connectToDaemon({ url: url('C'), auth: { apiKey }, onError: (e) => ev(`C onError ${e.message}`) });
    sessionC = await C.sessions.resume(sessionA.id, handlers('C', 9000, 9000));
    ev(`C resumed session mid-turn ${sessionC.id}`);
    globalThis.__C = C;
  } catch (e) {
    ev(`C failed: ${e.message}`);
  }
}, 8000);

const prompt =
  'First run the shell command `echo spike-hello` and show me its output. ' +
  'Then use the ask_user tool to ask me one question: "Which colour do you prefer?" with options red and blue. ' +
  'Finally reply with one sentence that repeats my answer. Do nothing else.';

ev('A streaming prompt');
try {
  for await (const message of sessionA.stream(prompt)) {
    if (message.type === DroidMessageType.Assistant) ev(`A assistant: ${message.text?.slice(0, 200)}`);
    else ev(`A stream message type=${message.type}`);
  }
  ev('A turn finished');
} catch (e) {
  ev(`A stream error: ${e.message}`);
}

await new Promise((r) => setTimeout(r, 4000));

// Second experiment: B streams a turn while A is idle, to see whether the
// stream direction matters for who gets asked.
ev('B streaming prompt');
try {
  for await (const message of sessionB.stream('Run the shell command `echo second-turn` and show me its output. Nothing else.')) {
    if (message.type === DroidMessageType.Assistant) ev(`B assistant: ${message.text?.slice(0, 200)}`);
    else ev(`B stream message type=${message.type}`);
  }
  ev('B turn finished');
} catch (e) {
  ev(`B stream error: ${e.message}`);
}

await new Promise((r) => setTimeout(r, 2000));
ev('closing');
await Promise.allSettled([sessionA.close()]);
A.disconnect();
B.disconnect();
globalThis.__C?.disconnect();
wss.close();
daemon.kill();
process.exit(0);
