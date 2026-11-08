import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * THIRD-PARTY-NOTICES.md for the Kernel's executable: the licenses of everything `bun build --compile` puts in it.
 *
 * The list is read from the bundle itself — the packages its source map names — so a dependency added or dropped
 * is in it or out of it without anyone keeping a list. Each package brings the license files it ships (LICENSE,
 * NOTICE, and the LICENSES/ folder of code it vendors). A package that ships none gets the upstream text kept in
 * `src/vendor/licenses/`; one that has neither stops the build, because MIT, BSD, ISC and Apache all ask for their
 * text to go with a binary. Bun's own license comes first: the executable is Bun.
 */

const root = new URL('.', import.meta.url).pathname
const vendored = join(root, 'src', 'vendor', 'licenses')

/** Upstream texts for packages that ship none, by package name or scope. */
const upstream: { match: (name: string) => boolean; file: string; note: string }[] = [
  {
    match: (name) => name.startsWith('@earendil-works/'),
    file: 'earendil-works-pi.txt',
    note: 'From the LICENSE of github.com/earendil-works/pi, the repository the package names.',
  },
  {
    match: (name) => name === 'standardwebhooks',
    file: 'standard-webhooks.txt',
    note: 'The package declares MIT and ships no text; this is the LICENSE of github.com/standard-webhooks/standard-webhooks, the repository it names.',
  },
]

/** Licenses that ask nothing of a binary. */
const noConditions = new Set(['Unlicense', '0BSD', 'CC0-1.0'])

const licenseFile = /^(licen[cs]e|copying|notice)(\.|-|$)/i

export type BundledPackage = { name: string; version: string; license: string; dir: string }

/** The packages the Kernel's bundle holds code of, by the source map of an in-memory build. */
export async function bundledPackages(): Promise<BundledPackage[]> {
  const result = await Bun.build({
    entrypoints: [join(root, 'src/main.ts'), join(root, 'src/codemode-worker.ts')],
    target: 'bun',
    // No outdir: the build stays in memory.
    sourcemap: 'external',
  })
  if (!result.success) throw new Error(`the bundle failed: ${result.logs.join('\n')}`)
  const dirs = new Map<string, string>()
  for (const output of result.outputs) {
    if (output.kind !== 'sourcemap') continue
    const map = JSON.parse(await output.text()) as { sources: string[] }
    for (const source of map.sources) {
      const at = source.lastIndexOf('node_modules/')
      if (at < 0) continue
      const parts = source.slice(at + 'node_modules/'.length).split('/')
      const name = parts[0]!.startsWith('@') ? `${parts[0]}/${parts[1]}` : parts[0]!
      dirs.set(name, join(root, source.slice(0, at + 'node_modules/'.length) + name))
    }
  }
  return [...dirs]
    .map(([name, dir]) => {
      const manifest = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8')) as {
        version?: string
        license?: string
      }
      return { name, version: manifest.version ?? '', license: manifest.license ?? 'UNKNOWN', dir }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** The license texts of one package, as `[heading, text]` pairs; none for a license without conditions. */
export function licenseTexts(pkg: BundledPackage): [string, string][] {
  const own = readdirSync(pkg.dir)
    .filter((file) => licenseFile.test(file))
    .sort()
    .map((file): [string, string] => [file, readFileSync(join(pkg.dir, file), 'utf8')])
  const licenses = join(pkg.dir, 'LICENSES')
  if (existsSync(licenses)) {
    for (const file of readdirSync(licenses).sort()) {
      own.push([
        `LICENSES/${file} (code the package includes)`,
        readFileSync(join(licenses, file), 'utf8'),
      ])
    }
  }
  if (own.some(([file]) => !file.startsWith('LICENSES/'))) return own
  const kept = upstream.find((each) => each.match(pkg.name))
  if (kept !== undefined) {
    return [[kept.note, readFileSync(join(vendored, kept.file), 'utf8')], ...own]
  }
  if (noConditions.has(pkg.license)) return own
  throw new Error(
    `${pkg.name}@${pkg.version} (${pkg.license}) ships no license text: add its upstream text to src/vendor/licenses/ and to \`upstream\` in notices.ts`,
  )
}

export async function thirdPartyNotices(): Promise<string> {
  const packages = await bundledPackages()
  const lines = [
    '# Third-party notices for the Droi Kernel',
    '',
    "The Kernel is a Bun executable (`bun build --compile`). It contains Bun, and the code of the packages below. Each package's license follows its name. The WebAssembly the Kernel embeds (Photon, QuickJS) is in NOTICE.md beside this file.",
    '',
    '## Bun 1.3.14',
    '',
    readFileSync(join(vendored, 'bun.md'), 'utf8').trim().replaceAll(/^## /gm, '### '),
    '',
  ]
  for (const pkg of packages) {
    lines.push(`## ${pkg.name} ${pkg.version} (${pkg.license})`, '')
    for (const [heading, body] of licenseTexts(pkg)) {
      lines.push(`### ${heading}`, '', '```text', body.trim().replaceAll('```', "'''"), '```', '')
    }
  }
  return lines.join('\n')
}

if (import.meta.main) {
  process.stdout.write(await thirdPartyNotices())
}
