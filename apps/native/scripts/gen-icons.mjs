// Writes the native app's Lucide icons as SVG files: every icon already in
// internal/icons/svg, plus those named on the command line, from the
// lucide-react-native release the Phone App draws with.
//   node apps/native/scripts/gen-icons.mjs [icon-name ...]
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'

const root = resolve(import.meta.dirname, '../../..')
const out = resolve(import.meta.dirname, '../internal/icons/svg')
const icons = join(root, 'apps/mobile/node_modules/lucide-react-native/dist/esm/icons')

mkdirSync(out, { recursive: true })
const names = new Set([
  ...readdirSync(out)
    .filter((f) => f.endsWith('.svg'))
    .map((f) => f.slice(0, -4)),
  ...process.argv.slice(2),
])
for (const name of [...names].sort()) {
  // Each icon module holds its data as an object literal; importing the module
  // would pull in React Native.
  const source = readFileSync(join(icons, `${name}.mjs`), 'utf8')
  const literal = /const iconData = (\{[\s\S]*?\n\});/.exec(source)?.[1]
  if (!literal) throw new Error(`no icon data in ${name}.mjs`)
  /** @type {{ node: [string, Record<string, string>][] }} */
  const data = new Function(`return (${literal})`)()
  const body = data.node
    .map(([tag, attrs]) => {
      const a = Object.entries(attrs)
        .filter(([k]) => k !== 'key')
        .map(([k, v]) => `${k}="${v}"`)
        .join(' ')
      return `<${tag} ${a}/>`
    })
    .join('')
  writeFileSync(
    join(out, `${name}.svg`),
    `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${body}</svg>\n`,
  )
}
console.log(`${names.size} icons in ${relative(root, out)}`)
