const d = JSON.parse(require('fs').readFileSync(process.argv[2], 'utf8'))
const keys = new Set()
for (const e of d) Object.keys(e).forEach((k) => keys.add(k))
console.log('keys:', [...keys].join(','))
for (const e of d) {
  const b = e.box.map((v) => Math.round(v * 10) / 10).join(',')
  const parts = [e.tag, `[${b}]`]
  if (e.role) parts.push(`role=${e.role}`)
  if (e.label) parts.push(`"${e.label}"`)
  if (e.text) parts.push(`T"${e.text.slice(0, 60)}"`)
  for (const k of Object.keys(e))
    if (!['tag', 'box', 'role', 'label', 'text'].includes(k))
      parts.push(`${k}=${typeof e[k] === 'object' ? JSON.stringify(e[k]) : e[k]}`)
  console.log(parts.join(' '))
}
