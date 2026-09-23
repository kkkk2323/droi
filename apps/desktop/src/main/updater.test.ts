import { createHash } from 'node:crypto'
import * as fs from 'node:fs'
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createUpdater, isNewerVersion, parseManifest, type UpdaterFs } from './updater'

const nodeFs: UpdaterFs = {
  createWriteStream: (p) => fs.createWriteStream(p),
  createReadStream: (p) => fs.createReadStream(p),
  copyFile: fs.promises.copyFile,
  rename: fs.promises.rename,
  unlink: fs.promises.unlink,
}

describe('isNewerVersion', () => {
  it('compares dotted numbers, ignoring a leading v', () => {
    expect(isNewerVersion('1.0.0', '0.33.2')).toBe(true)
    expect(isNewerVersion('v1.0.1', '1.0.0')).toBe(true)
    expect(isNewerVersion('1.0.0', '1.0.0')).toBe(false)
    expect(isNewerVersion('0.9.9', '1.0.0')).toBe(false)
    expect(isNewerVersion('1.0', '1.0.0')).toBe(false)
    expect(isNewerVersion('abc', '1.0.0')).toBe(false)
  })
})

describe('parseManifest', () => {
  it('accepts the workflow output and rejects anything short of it', () => {
    const sha = 'a'.repeat(64)
    expect(parseManifest({ version: '1.2.3', sha256: sha.toUpperCase() })).toEqual({
      version: '1.2.3',
      sha256: sha,
    })
    expect(parseManifest({ version: 'latest', sha256: sha })).toBeNull()
    expect(parseManifest({ version: '1.2.3', sha256: 'nope' })).toBeNull()
    expect(parseManifest('1.2.3')).toBeNull()
  })
})

describe('createUpdater', () => {
  let dir: string
  let asarPath: string
  const newAsar = Buffer.from('new app code')
  const archive = gzipSync(newAsar)
  const sha256 = createHash('sha256').update(archive).digest('hex')
  const responses = new Map<string, () => Response>()
  const fetch = async (url: string) => {
    const make = responses.get(url)
    return make ? make() : new Response('not found', { status: 404 })
  }

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), 'droi-updater-'))
    asarPath = join(dir, 'app.asar')
    await writeFile(asarPath, 'old app code')
    responses.clear()
  })
  afterEach(() => rm(dir, { recursive: true, force: true }))

  const updater = (currentVersion = '1.0.0') =>
    createUpdater({
      currentVersion,
      repository: 'acme/droi',
      asarPath,
      downloadDir: dir,
      fetch,
      fs: nodeFs,
    })

  it('reports a newer release as available and the same one as up to date', async () => {
    responses.set('https://github.com/acme/droi/releases/latest/download/latest.json', () =>
      Response.json({ version: '1.1.0', sha256 }),
    )
    expect(await updater('1.0.0').check()).toEqual({ status: 'available', version: '1.1.0' })
    expect(await updater('1.1.0').check()).toEqual({ status: 'up-to-date', version: '1.1.0' })
  })

  it('turns a missing or malformed manifest into an error state', async () => {
    expect(await updater().check()).toMatchObject({ status: 'error', message: /HTTP 404/ })
    responses.set('https://github.com/acme/droi/releases/latest/download/latest.json', () =>
      Response.json({ version: '1.1.0' }),
    )
    expect(await updater().check()).toMatchObject({ status: 'error', message: /malformed/ })
  })

  it('downloads, verifies, swaps the archive and keeps the old one', async () => {
    responses.set('https://github.com/acme/droi/releases/latest/download/latest.json', () =>
      Response.json({ version: '1.1.0', sha256 }),
    )
    responses.set(
      'https://github.com/acme/droi/releases/download/v1.1.0/app.asar.gz',
      () => new Response(archive, { headers: { 'content-length': String(archive.byteLength) } }),
    )
    const u = updater()
    const states: string[] = []
    u.on('change', (s) => states.push(s.status))
    await u.check()
    expect(await u.install()).toEqual({ status: 'ready', version: '1.1.0' })
    expect(await readFile(asarPath, 'utf8')).toBe('new app code')
    expect(await readFile(join(dir, 'app.previous.asar'), 'utf8')).toBe('old app code')
    expect(fs.existsSync(join(dir, 'update.asar.gz'))).toBe(false)
    expect(states).toContain('downloading')
  })

  it('refuses an archive whose hash differs and leaves the app alone', async () => {
    responses.set('https://github.com/acme/droi/releases/latest/download/latest.json', () =>
      Response.json({ version: '1.1.0', sha256: 'b'.repeat(64) }),
    )
    responses.set(
      'https://github.com/acme/droi/releases/download/v1.1.0/app.asar.gz',
      () => new Response(archive),
    )
    const u = updater()
    await u.check()
    expect(await u.install()).toMatchObject({ status: 'error', message: /does not match/ })
    expect(await readFile(asarPath, 'utf8')).toBe('old app code')
    expect(fs.existsSync(join(dir, 'update.asar'))).toBe(false)
  })

  it('does nothing when asked to install without an available update', async () => {
    const u = updater()
    expect(await u.install()).toEqual({ status: 'idle' })
  })
})
