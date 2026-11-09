// What upstream's plugin sends Factory for each request of requests.json, signed in with a key as Droi's Key Pool
// is, and what it sends again when Factory answers the first with a 413. Writes fixture.json.
//
//   UPSTREAM=<magpie-community/plugins checkout>/packages/factory bun run upstream.mjs
// at the commit PORTED-FROM.md names.
import { unlink } from 'node:fs/promises'
const here = new URL('.', import.meta.url).pathname
const requests = JSON.parse(await Bun.file(here + 'requests.json').text())
const API = 'https://api.factory.ai'
let sent = [],
  tooLarge = false
globalThis.fetch = async (url, init = {}) => {
  const u = new URL(String(url))
  if (u.pathname === '/api/cli/whoami')
    return new Response(
      JSON.stringify({ userId: 'user_k', orgId: 'org_1', email: 'k@example.com', region: '' }),
    )
  const headers = {}
  new Headers(init.headers).forEach((v, k) => (headers[k] = v))
  let body = init.body
  if (body != null && typeof body !== 'string') body = new TextDecoder().decode(body)
  // a header that differs on every request says nothing of how it was built
  for (const k of ['authorization', 'x-session-id', 'x-assistant-message-id']) delete headers[k]
  sent.push({ path: u.pathname + u.search, headers, body })
  if (tooLarge && sent.length === 1)
    return new Response('{"error":{"message":"Request Entity Too Large"}}', {
      status: 413,
      headers: { 'content-type': 'application/json' },
    })
  return new Response('{"id":"ok"}', { headers: { 'content-type': 'application/json' } })
}
const { FactoryAuthPlugin } = await import(process.env.UPSTREAM + '/index.mjs')
let saved = { type: 'api', key: 'fk-test-0123456789', metadata: { region: '' } }
const hooks = await FactoryAuthPlugin({
  client: { auth: { set: async ({ body }) => (saved = body) } },
})
const l = await hooks.auth.loader(async () => saved)
const run = async (r, large) => {
  sent = []
  tooLarge = large
  const res = await l.fetch(API + r.path, { method: 'POST', headers: r.headers, body: r.body })
  await res.text()
  return sent
}
const out = []
for (const r of requests) {
  const [first] = await run(r, false)
  const again = await run(r, true)
  out.push({
    ...r,
    sent: first,
    resent: again.length > 1 ? { path: again[1].path, body: again[1].body } : null,
  })
}
// Every refused phrase with its first character as a \u escape, which JSON reads as the same text: reading the
// fixture from a session on Factory does not get it refused.
const phrases = JSON.parse(await Bun.file(here + '../client-phrases.json').text())
let text = '[\n' + out.map((c) => JSON.stringify(c)).join(',\n') + '\n]\n'
for (const p of [...phrases.anywhere, ...phrases.toolOnly].sort((a, b) => b.length - a.length)) {
  text = text.replaceAll(p, '\\u' + p.charCodeAt(0).toString(16).padStart(4, '0') + p.slice(1))
}
await Bun.write(here + 'fixture.json', text)
await unlink(here + 'requests.json')
console.log(out.length, 'cases,', out.filter((c) => c.resent).length, 'sent again on a 413')
