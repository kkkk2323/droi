import { describe, expect, test } from 'bun:test'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { type BundledPackage, licenseTexts } from '../notices.ts'

const root = join(import.meta.dir, '..')

/** notices.ts in a process of its own: a build inside the test runner's process fails to read files. */
function run(...args: string[]): string {
  const result = Bun.spawnSync(['bun', 'notices.ts', ...args], { cwd: root })
  if (result.exitCode !== 0) throw new Error(result.stderr.toString())
  return result.stdout.toString()
}

describe('third-party notices', () => {
  test('name every package the bundle holds, each with a license text', () => {
    const packages = JSON.parse(run('--packages')) as BundledPackage[]
    expect(packages.map((pkg) => pkg.name)).toContain('@earendil-works/pi-durable')
    expect(packages.map((pkg) => pkg.name)).toContain('diff')
    const notices = run()
    expect(notices).toContain('Bun itself is MIT-licensed.')
    for (const pkg of packages) {
      expect(notices).toContain(`## ${pkg.name} ${pkg.version} (${pkg.license})`)
      if (pkg.license !== 'Unlicense') expect(licenseTexts(pkg).length).toBeGreaterThan(0)
    }
    // The code a package vendors brings its own notice.
    expect(notices).toContain('LICENSES/typebox-LICENSE')
  }, 60_000)

  test('a package without a license text of its own or upstream stops the build', () => {
    const dir = mkdtempSync(join(tmpdir(), 'notice-'))
    try {
      mkdirSync(join(dir, 'pkg'))
      writeFileSync(join(dir, 'pkg', 'package.json'), '{"version":"1.0.0","license":"MIT"}')
      expect(() =>
        licenseTexts({ name: 'nobody', version: '1.0.0', license: 'MIT', dir: join(dir, 'pkg') }),
      ).toThrow('ships no license text')
      expect(
        licenseTexts({
          name: 'free',
          version: '1.0.0',
          license: 'Unlicense',
          dir: join(dir, 'pkg'),
        }),
      ).toEqual([])
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
})
