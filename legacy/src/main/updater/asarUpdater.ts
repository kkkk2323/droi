import { app, net } from 'electron'
import * as fs from 'original-fs'
import { createHash } from 'crypto'
import { createGunzip } from 'zlib'
import { pipeline } from 'stream/promises'
import { join, dirname } from 'path'
import { EventEmitter } from 'events'

export interface UpdateCheckResult {
  available: boolean
  version?: string
  currentVersion?: string
}

export interface DownloadProgress {
  percent: number
  transferred: number
  total: number
}

export type UpdaterStatus =
  | 'idle'
  | 'checking'
  | 'available'
  | 'not-available'
  | 'downloading'
  | 'downloaded'
  | 'extracting'
  | 'moving'
  | 'ready'
  | 'error'

export class AsarUpdater extends EventEmitter {
  private owner = 'kkkk2323'
  private repo = 'droi'
  private status: UpdaterStatus = 'idle'
  private downloadDir: string
  private updateInfo: { version: string; sha256: string; downloadUrl: string } | null = null
  private errorMessage = ''

  constructor() {
    super()
    this.downloadDir = app.getPath('userData')
  }

  getStatus(): UpdaterStatus {
    return this.status
  }

  getErrorMessage(): string {
    return this.errorMessage
  }

  async check(): Promise<UpdateCheckResult> {
    this.setStatus('checking')
    this.errorMessage = ''
    const currentVersion = app.getVersion()

    try {
      const url = `https://github.com/${this.owner}/${this.repo}/releases/latest/download/latest.json`
      const resp = await net.fetch(url)
      if (!resp.ok) {
        this.setStatus('error')
        this.errorMessage = `Failed to fetch update info: HTTP ${resp.status}`
        throw new Error(this.errorMessage)
      }

      const data = (await resp.json()) as { version?: string; sha256?: string }
      if (!data.version || !data.sha256) {
        this.setStatus('error')
        this.errorMessage = 'Invalid update manifest'
        throw new Error(this.errorMessage)
      }

      if (!this.isNewer(data.version, currentVersion)) {
        this.setStatus('not-available')
        return { available: false, version: data.version, currentVersion }
      }

      this.updateInfo = {
        version: data.version,
        sha256: data.sha256,
        downloadUrl: `https://github.com/${this.owner}/${this.repo}/releases/download/v${data.version.replace(/^v/, '')}/app.asar.gz`,
      }
      this.setStatus('available')
      return { available: true, version: data.version, currentVersion }
    } catch (err) {
      if (this.status !== 'error') {
        this.setStatus('error')
        this.errorMessage = err instanceof Error ? err.message : String(err)
      }
      throw err
    }
  }

  async downloadAndInstall(): Promise<void> {
    if (!this.updateInfo) throw new Error('No update available. Call check() first.')
    this.errorMessage = ''

    const { downloadUrl, sha256 } = this.updateInfo
    const gzPath = join(this.downloadDir, 'update.asar.gz')
    const asarPath = join(this.downloadDir, 'update.asar')

    try {
      this.setStatus('downloading')
      await this.download(downloadUrl, gzPath)

      this.setStatus('downloaded')
      const fileHash = await this.hashFile(gzPath)
      if (fileHash !== sha256) {
        this.setStatus('error')
        this.errorMessage = `Hash mismatch: expected ${sha256.slice(0, 12)}..., got ${fileHash.slice(0, 12)}...`
        throw new Error(this.errorMessage)
      }

      this.setStatus('extracting')
      await this.gunzip(gzPath, asarPath)

      this.setStatus('moving')
      await this.replaceAsar(asarPath)

      this.setStatus('ready')
    } catch (err) {
      if (this.status !== 'error') {
        this.setStatus('error')
        this.errorMessage = err instanceof Error ? err.message : String(err)
      }
      throw err
    }
  }

  relaunch(): void {
    app.relaunch()
    app.quit()
  }

  private async download(url: string, dest: string): Promise<void> {
    const resp = await net.fetch(url)
    if (!resp.ok || !resp.body) throw new Error(`Download failed: HTTP ${resp.status}`)

    const total = Number(resp.headers.get('content-length') || 0)
    let transferred = 0

    const writeStream = fs.createWriteStream(dest)
    const reader = resp.body.getReader()

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        writeStream.write(value)
        transferred += value.byteLength
        this.emit('progress', {
          percent: total > 0 ? transferred / total : 0,
          transferred,
          total,
        } satisfies DownloadProgress)
      }
    } finally {
      await new Promise<void>((resolve, reject) => {
        writeStream.end(() => resolve())
        writeStream.on('error', reject)
      })
    }
  }

  private async hashFile(filePath: string): Promise<string> {
    const hash = createHash('sha256')
    const stream = fs.createReadStream(filePath)
    for await (const chunk of stream) {
      hash.update(chunk)
    }
    return hash.digest('hex')
  }

  private async gunzip(src: string, dest: string): Promise<void> {
    const input = fs.createReadStream(src)
    const gunzip = createGunzip()
    const output = fs.createWriteStream(dest)
    await pipeline(input, gunzip, output)
  }

  private async replaceAsar(updateAsarPath: string): Promise<void> {
    const resourcesDir = app.isPackaged ? dirname(app.getAppPath()) : app.getAppPath()
    const appAsarPath = join(resourcesDir, 'app.asar')
    const bakAsarPath = join(this.downloadDir, 'app.bak.asar')

    if (app.isPackaged) {
      try {
        await fs.promises.unlink(bakAsarPath).catch(() => {})
        await fs.promises.copyFile(appAsarPath, bakAsarPath)
      } catch {
        // backup is best-effort
      }
    }

    await fs.promises.copyFile(updateAsarPath, appAsarPath)

    // cleanup temp files
    await fs.promises.unlink(updateAsarPath).catch(() => {})
    const gzPath = updateAsarPath.replace(/\.asar$/, '.asar.gz')
    await fs.promises.unlink(gzPath).catch(() => {})
  }

  private isNewer(remote: string, local: string): boolean {
    const parse = (v: string) => v.replace(/^v/, '').split('.').map(Number)
    const r = parse(remote)
    const l = parse(local)
    for (let i = 0; i < Math.max(r.length, l.length); i++) {
      const a = r[i] || 0
      const b = l[i] || 0
      if (a > b) return true
      if (a < b) return false
    }
    return false
  }

  private setStatus(status: UpdaterStatus): void {
    this.status = status
    this.emit('status', status)
  }
}
