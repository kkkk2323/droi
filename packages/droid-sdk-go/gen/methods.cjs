// Reads DaemonClient's bundled source and lists, per method, the JSON-RPC
// method, its request and result schemas, whether it is ack-compatible and
// which notification completes it, and any timeout override.
const fs = require('fs')
const src = fs.readFileSync(process.argv[2], 'utf8')
const lines = src.split('\n')
const methods = []
let cur = null
for (const l of lines) {
  const m = /^  (?:async )?(\w+)\(([^)]*)\) \{$/.exec(l)
  if (m && !['constructor', 'if', 'for', 'switch'].includes(m[1])) {
    cur = { name: m[1], args: m[2], body: [] }
    methods.push(cur)
    continue
  }
  if (cur) cur.body.push(l)
}
const out = []
const odd = []
for (const m of methods) {
  const b = m.body.join('\n')
  const rpc = [...b.matchAll(/method: "([a-z_.]+)"/g)].map((x) => x[1])
  if (rpc.length === 0) continue
  const req = /(\w+RequestSchema\w*)\.parse/.exec(b)?.[1]
  const res =
    /sendRequest\(\s*validated,\s*(\w+)/.exec(b)?.[1] ?? /responseSchema: (\w+)/.exec(b)?.[1]
  const ack = /sendAckCompatibleRequest/.test(b)
  const done = [...b.matchAll(/notification\.type === "(\w+)"/g)].map((x) => x[1])
  const timeout =
    /sendRequest\(\s*validated,\s*\w+,\s*([^)]+?)\s*\)/.exec(b)?.[1]?.trim() ??
    /timeoutOverride: ([^,}\n]+)/.exec(b)?.[1]?.trim()
  const e = { name: m.name, args: m.args, rpc: rpc[0], req, res, ack, done, timeout }
  if (rpc.length > 1 || !req || !res) odd.push({ ...e, rpcs: rpc })
  out.push(e)
}
fs.writeFileSync(process.argv[3], JSON.stringify(out, null, 1))
console.log('methods', out.length, 'odd', odd.length)
for (const o of odd) console.log('ODD', JSON.stringify(o))
const timeouts = [...new Set(out.map((o) => o.timeout).filter(Boolean))]
console.log('timeouts', timeouts.join(' | '))
console.log(
  'ack',
  out
    .filter((o) => o.ack)
    .map((o) => o.name + ':' + o.done.join('/'))
    .join(' '),
)
