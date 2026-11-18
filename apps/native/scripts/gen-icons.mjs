// Writes the native app's Lucide icons as SVG files: every icon already in
// pkg/icons/svg, plus those named on the command line, from the lucide-static
// release in the workspace's devDependencies.
//   node apps/native/scripts/gen-icons.mjs [icon-name ...]
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'

const root = resolve(import.meta.dirname, '../../..')
const out = resolve(import.meta.dirname, '../pkg/icons/svg')
const icons = join(root, 'node_modules/lucide-static/icons')

mkdirSync(out, { recursive: true })
const names = new Set([
  ...readdirSync(out)
    .filter((f) => f.endsWith('.svg'))
    .map((f) => f.slice(0, -4)),
  ...process.argv.slice(2),
])
for (const name of [...names].sort()) {
  const source = readFileSync(join(icons, `${name}.svg`), 'utf8')
  // lucide-static adds a class and a license comment; the icons keep only the
  // drawing, on one line, as pkg/icons reads them.
  const body = /<svg[^>]*>([\s\S]*)<\/svg>/.exec(source)?.[1]
  if (!body) throw new Error(`no drawing in ${name}.svg`)
  const shapes = body
    .replace(/<!--[\s\S]*?-->/g, '')
    .split('\n')
    .map((line) => line.trim())
    .join('')
    .replace(/\s*\/>/g, '/>')
  writeFileSync(
    join(out, `${name}.svg`),
    `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${shapes}</svg>\n`,
  )
}
console.log(`${names.size} icons in ${relative(root, out)}`)
