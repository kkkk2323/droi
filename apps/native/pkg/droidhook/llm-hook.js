// Droi's LLM key hook (ADR 0033). Droi preloads it into Factory's own droid
// through BUN_OPTIONS: inside droid, a Factory LLM request gets the key that
// the Key Pool's key file holds, and every other request keeps droid's login.
const fs = require('fs')
const keyFile = process.env.DROI_LLM_KEY_FILE
const ready = process.env.DROI_HOOK_READY
// Every Bun process that inherits BUN_OPTIONS runs this file, such as a
// script an agent starts. Only droid's own executable is changed.
const main = typeof Bun !== 'undefined' ? String(Bun.main ?? '') : ''
const isDroid = /^(\/\$bunfs\/|[A-Za-z]:[\\/]~BUN[\\/])/.test(main)
if (keyFile && isDroid) {
  const hosts = new Set(
    [
      process.env.FACTORY_API_BASE_URL || 'https://api.factory.ai',
      process.env.FACTORY_API_BASE_URL_EU,
    ]
      .filter(Boolean)
      .map((u) => {
        try {
          return new URL(u).host
        } catch {
          return ''
        }
      }),
  )
  const route = /(^|\/)api\/llm\/(a|o|g|v1|decision)\//
  // An account with a premium host sends its LLM requests to that host
  // (prem.factory.ai), not to FACTORY_API_BASE_URL.
  const factoryHost = (h) => h === 'factory.ai' || h.endsWith('.factory.ai')
  const isLLM = (u) =>
    (hosts.has(u.host) || factoryHost(u.hostname)) &&
    route.test(u.pathname) &&
    !/\/api\/llm\/custom\//.test(u.pathname)
  const readKey = () => {
    let k = ''
    try {
      k = fs.readFileSync(keyFile, 'utf8').trim()
    } catch {}
    // Never the login instead: that would bill the signed-in account.
    if (!/^fk-\S+$/.test(k))
      throw Object.assign(new Error("Droi's LLM key file is missing or holds no Factory API key"), {
        status: 400,
      })
    return k
  }
  const origFetch = globalThis.fetch
  const wrapped = function (input, init) {
    let url
    try {
      url = new URL(input instanceof Request ? input.url : String(input))
    } catch {
      return origFetch(input, init)
    }
    if (!isLLM(url)) return origFetch(input, init)
    let key
    try {
      key = readKey()
    } catch (e) {
      return Promise.reject(e)
    }
    if (input instanceof Request) {
      const h = new Headers(input.headers)
      if (init?.headers) new Headers(init.headers).forEach((v, n) => h.set(n, v))
      h.set('Authorization', `Bearer ${key}`)
      return origFetch(new Request(input, { ...init, headers: h }))
    }
    const h = new Headers(init?.headers)
    h.set('Authorization', `Bearer ${key}`)
    return origFetch(input, { ...init, headers: h })
  }
  Object.assign(wrapped, origFetch)
  globalThis.fetch = wrapped
  const OrigWS = globalThis.WebSocket
  if (OrigWS) {
    globalThis.WebSocket = class extends OrigWS {
      constructor(url, opts) {
        let u
        try {
          u = new URL(String(url))
        } catch {}
        if (u && isLLM(u) && opts && typeof opts === 'object' && !Array.isArray(opts)) {
          const h = new Headers(opts.headers)
          h.set('Authorization', `Bearer ${readKey()}`)
          opts = { ...opts, headers: Object.fromEntries(h) }
        }
        super(url, opts)
      }
    }
  }
  if (ready) {
    // A droid this one starts must not answer for it.
    delete process.env.DROI_HOOK_READY
    try {
      fs.writeFileSync(ready, JSON.stringify({ pid: process.pid }))
    } catch {}
  }
}
