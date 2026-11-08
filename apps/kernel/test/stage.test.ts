import { describe, expect, test } from 'bun:test'
import { mkdtempSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { stage } from '../build.ts'

describe('stage', () => {
  test('puts each executable in its Go platform folder with the notices beside it', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'kernel-stage-'))
    try {
      const built = [
        { platform: 'darwin-x64', file: 'kernel' },
        { platform: 'windows-x64', file: 'kernel.exe' },
        { platform: 'linux-arm64', file: 'kernel' },
      ].map(({ platform, file }) => {
        const path = join(dir, `${platform}-${file}`)
        writeFileSync(path, platform)
        return { platform, path, name: file }
      })
      const staged = await stage(
        built.map(({ platform, path }) => ({ platform, path })),
        join(dir, 'resources'),
        '# notices',
      )
      expect(staged.map((path) => path.slice(dir.length + 1))).toEqual([
        'resources/darwin-amd64/darwin-x64-kernel',
        'resources/windows-amd64/windows-x64-kernel.exe',
        'resources/linux-arm64/linux-arm64-kernel',
      ])
      for (const path of staged) {
        expect(statSync(path).mode & 0o111).not.toBe(0)
        const notices = readdirSync(join(path, '..', 'kernel-licenses')).sort()
        expect(notices).toEqual([
          'LICENSE-photon.md',
          'LICENSE-photon.txt',
          'LICENSE-quickjs-wasi.txt',
          'NOTICE.md',
          'THIRD-PARTY-NOTICES.md',
        ])
      }
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  }, 60_000)
})
