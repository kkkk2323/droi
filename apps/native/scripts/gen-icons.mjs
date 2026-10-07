// Writes the Lucide icons the web Client imports as SVG files for the native
// app, so both draw the same glyphs from the same lucide-react release.
//   node apps/native/scripts/gen-icons.mjs
import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, relative, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

const root = resolve(import.meta.dirname, '../../..')
const renderer = join(root, 'apps/desktop/src/renderer/src')
const out = resolve(import.meta.dirname, '../internal/icons/svg')
const require = createRequire(join(root, 'apps/desktop/package.json'))
const lucide = dirname(require.resolve('lucide-react/package.json'))
const index = readFileSync(join(lucide, 'dist/esm/lucide-react.mjs'), 'utf8')

// Export name -> icon file, from lines like
// export { default as Loader2, default as LoaderCircle } from './icons/loader-circle.mjs';
const files = new Map()
for (const m of index.matchAll(/export \{([^}]+)\} from '\.\/icons\/([a-z0-9-]+)\.mjs'/g)) {
  for (const name of m[1].matchAll(/default as (\w+)/g)) files.set(name[1], m[2])
}

function sources(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name)
    if (e.isDirectory()) return sources(p)
    return /\.tsx?$/.test(e.name) && !/\.test\./.test(e.name) ? [p] : []
  })
}

// Streamdown's code block draws Download and Copy itself, outside these imports.
const used = new Set([
  'Loader2',
  'PanelLeft',
  'X',
  'ChevronRight',
  'ChevronDown',
  'Download',
  'Copy',
  // The native Automations page, which the web Client does not have.
  'CalendarClock',
  'CirclePause',
  'Ellipsis',
  'Play',
  'Trash2',
])
for (const file of sources(renderer)) {
  for (const m of readFileSync(file, 'utf8').matchAll(
    /import\s*\{([^}]+)\}\s*from\s*'lucide-react'/g,
  )) {
    for (const part of m[1].split(',')) {
      const name = part
        .replace(/\btype\b/, '')
        .split(/\s+as\s+/)[0]
        .trim()
      if (name && files.has(name)) used.add(name)
    }
  }
}

rmSync(out, { recursive: true, force: true })
mkdirSync(out, { recursive: true })
const written = new Set()
for (const name of [...used].sort()) {
  const file = files.get(name)
  if (written.has(file)) continue
  written.add(file)
  const mod = await import(pathToFileURL(join(lucide, 'dist/esm/icons', `${file}.mjs`)).href)
  const body = mod.__iconData.node
    .map(([tag, attrs]) => {
      const a = Object.entries(attrs)
        .filter(([k]) => k !== 'key')
        .map(([k, v]) => `${k}="${v}"`)
        .join(' ')
      return `<${tag} ${a}/>`
    })
    .join('')
  writeFileSync(
    join(out, `${file}.svg`),
    `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${body}</svg>\n`,
  )
}
console.log(`${written.size} icons in ${relative(root, out)}`)
