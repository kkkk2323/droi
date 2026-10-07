const fs = require('fs'),
  path = require('path')
const [src, out] = process.argv.slice(2)
const text = fs.readFileSync(src, 'utf8')
const lines = text.split('\n')
let cur = '_prelude',
  buf = []
const sizes = {}
const flush = () => {
  if (!buf.length) return
  const p = path.join(out, cur.replace(/^(\.\.\/)+/, '').replace(/[^\w./-]/g, '_') + '.js')
  fs.mkdirSync(path.dirname(p), { recursive: true })
  fs.appendFileSync(p, buf.join('\n') + '\n')
  sizes[cur] = (sizes[cur] || 0) + buf.length
  buf = []
}
for (const l of lines) {
  const m = /^\/\/ (\S+\.(ts|tsx|js|mjs|cjs))$/.exec(l)
  if (m) {
    flush()
    cur = m[1]
    continue
  }
  buf.push(l)
}
flush()
for (const [k, v] of Object.entries(sizes).sort((a, b) => b[1] - a[1]))
  if (!k.includes('node_modules')) console.log(v, k)
