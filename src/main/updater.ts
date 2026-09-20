// In-app update for the Desktop Shell. The Release workflow attaches the
// packaged `app.asar` (gzipped) and a `latest.json` manifest to every GitHub
// Release; the Shell downloads the archive, checks its SHA-256 against the
// manifest and swaps it in for its own `app.asar`. The native Electron parts
// stay as they are, so this is an update of Droi's own code only; a new
// Electron needs a new DMG. Electron's asar-integrity fuse is off in our
// builds, and the bundle is ad-hoc signed, so a swapped archive still loads.
import { createHash } from 'node:crypto'
import { EventEmitter } from 'node:events'
import { createGunzip } from 'node:zlib'
import { pipeline } from 'node:stream/promises'
import { Readable } from 'node:stream'
import { join } from 'node:path'
import type { UpdateState } from '../shared/shell-settings'

export interface UpdaterFs {
  createWriteStream(path: string): NodeJS.WritableStream
  createReadStream(path: string): NodeJS.ReadableStream
  copyFile(from: string, to: string): Promise<void>
  rename(from: string, to: string): Promise<void>
  unlink(path: string): Promise<void>
}

export interface UpdaterOptions {
  currentVersion: string
  /** GitHub `owner/repo` the Releases live under. */
  repository: string
  /** Overrides `https://github.com/<repository>/releases` (local testing). */
  releaseBaseUrl?: string
  /** Where the running `app.asar` lives (Contents/Resources on macOS). */
  asarPath: string
  /** Scratch directory for the download and the backup of the old archive. */
  downloadDir: string
  fetch: (url: string) => Promise<Response>
  /** `original-fs` in Electron, which sees `.asar` files as files. */
  fs: UpdaterFs
}

export interface Updater extends EventEmitter<{ change: [UpdateState] }> {
  readonly state: UpdateState
  check(): Promise<UpdateState>
  install(): Promise<UpdateState>
}

interface Manifest {
  version: string
  sha256: string
}

/** True when `remote` is a higher semver-like `x.y.z` than `local`. */
export function isNewerVersion(remote: string, local: string): boolean {
  const parse = (v: string) => v.replace(/^v/, '').split('.').map(Number)
  const r = parse(remote)
  const l = parse(local)
  for (let i = 0; i < Math.max(r.length, l.length); i++) {
    const a = r[i] ?? 0
    const b = l[i] ?? 0
    if (Number.isNaN(a) || Number.isNaN(b)) return false
    if (a !== b) return a > b
  }
  return false
}

export function parseManifest(data: unknown): Manifest | null {
  if (typeof data !== 'object' || data === null) return null
  const { version, sha256 } = data as { version?: unknown; sha256?: unknown }
  if (typeof version !== 'string' || !/^\d+\.\d+\.\d+/.test(version)) return null
  if (typeof sha256 !== 'string' || !/^[0-9a-f]{64}$/i.test(sha256)) return null
  return { version: version.replace(/^v/, ''), sha256: sha256.toLowerCase() }
}

export function createUpdater(options: UpdaterOptions): Updater {
  const emitter = new EventEmitter<{ change: [UpdateState] }>() as Updater
  let state: UpdateState = { status: 'idle' }
  let manifest: Manifest | null = null
  let busy = false

  const set = (next: UpdateState) => {
    state = next
    emitter.emit('change', next)
  }
  const fail = (error: unknown): UpdateState => {
    set({ status: 'error', message: error instanceof Error ? error.message : String(error) })
    return state
  }
  const base = options.releaseBaseUrl ?? `https://github.com/${options.repository}/releases`
  const releaseUrl = (path: string) => `${base}/${path}`

  const check = async (): Promise<UpdateState> => {
    if (busy) return state
    busy = true
    set({ status: 'checking' })
    try {
      const response = await options.fetch(releaseUrl('latest/download/latest.json'))
      if (!response.ok) throw new Error(`Update check failed: HTTP ${response.status}`)
      const parsed = parseManifest(await response.json())
      if (!parsed) throw new Error('Update check failed: the release manifest is malformed')
      manifest = parsed
      if (isNewerVersion(parsed.version, options.currentVersion)) {
        set({ status: 'available', version: parsed.version })
      } else {
        set({ status: 'up-to-date', version: options.currentVersion })
      }
      return state
    } catch (error) {
      return fail(error)
    } finally {
      busy = false
    }
  }

  const download = async (url: string, dest: string, onPercent: (p: number) => void) => {
    const response = await options.fetch(url)
    if (!response.ok || !response.body) throw new Error(`Download failed: HTTP ${response.status}`)
    const total = Number(response.headers.get('content-length') ?? 0)
    let transferred = 0
    let lastPercent = -1
    const hash = createHash('sha256')
    const counted = new Readable({
      read() {},
    })
    const reader = response.body.getReader()
    const pump = (async () => {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        hash.update(value)
        transferred += value.byteLength
        const percent = total > 0 ? Math.floor((transferred / total) * 100) : 0
        if (percent !== lastPercent) {
          lastPercent = percent
          onPercent(percent)
        }
        counted.push(value)
      }
      counted.push(null)
    })()
    await Promise.all([pipeline(counted, options.fs.createWriteStream(dest)), pump])
    return hash.digest('hex')
  }

  const install = async (): Promise<UpdateState> => {
    if (busy) return state
    if (!manifest || state.status !== 'available') return state
    busy = true
    const { version, sha256 } = manifest
    const gzPath = join(options.downloadDir, 'update.asar.gz')
    const asarPath = join(options.downloadDir, 'update.asar')
    const backupPath = join(options.downloadDir, 'app.previous.asar')
    try {
      set({ status: 'downloading', version, percent: 0 })
      const digest = await download(
        releaseUrl(`download/v${version}/app.asar.gz`),
        gzPath,
        (percent) => set({ status: 'downloading', version, percent }),
      )
      if (digest !== sha256) {
        throw new Error('Download failed: the archive does not match the release manifest')
      }
      await pipeline(
        options.fs.createReadStream(gzPath),
        createGunzip(),
        options.fs.createWriteStream(asarPath),
      )
      // Keep the old archive next to the download; the swap itself is a rename
      // so a crash midway leaves either the old or the new file, never a torn one.
      await options.fs.copyFile(options.asarPath, backupPath)
      try {
        await options.fs.rename(asarPath, options.asarPath)
      } catch (error) {
        // Another volume (EXDEV): fall back to a copy.
        if ((error as NodeJS.ErrnoException).code !== 'EXDEV') throw error
        await options.fs.copyFile(asarPath, options.asarPath)
        await options.fs.unlink(asarPath).catch(() => {})
      }
      await options.fs.unlink(gzPath).catch(() => {})
      set({ status: 'ready', version })
      return state
    } catch (error) {
      await options.fs.unlink(gzPath).catch(() => {})
      await options.fs.unlink(asarPath).catch(() => {})
      return fail(error)
    } finally {
      busy = false
    }
  }

  Object.defineProperty(emitter, 'state', { get: () => state })
  emitter.check = check
  emitter.install = install
  return emitter
}
