import { describe, expect, test } from 'bun:test'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { bundledPackages, licenseTexts, thirdPartyNotices } from '../notices.ts'

describe('third-party notices', () => {
  test('name every package the bundle holds, each with a license text', async () => {
    const packages = await bundledPackages()
    expect(packages.map((pkg) => pkg.name)).toContain('@earendil-works/pi-durable')
    expect(packages.map((pkg) => pkg.name)).toContain('diff')
    const notices = await thirdPartyNotices()
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
