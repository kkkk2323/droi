import { mkdir, readdir, readFile, stat, unlink, writeFile, appendFile } from 'fs/promises'
import { dirname, join } from 'path'
import { platform } from 'os'
import { promptSig, redactJson, redactText, type PromptSig } from './redact.ts'
import { ZipBuilder } from './zip.ts'
import type { PersistedAppStateV2 } from '../../shared/protocol.ts'

export type DiagnosticsLevel = 'debug' | 'info' | 'warn' | 'error'
export type DiagnosticsScope = 'renderer' | 'main' | 'server' | 'backend' | 'droid-rpc'

export type DiagnosticsEvent = {
  ts: string
  level: DiagnosticsLevel
  scope: DiagnosticsScope
  event: string
  sessionId?: string
  correlation?: Record<string, unknown>
  data?: unknown
}

type RetentionPolicy = {
  maxAgeDays: number
  maxTotalBytes: number
}

type PendingFile = {
  buffer: string
  timer: NodeJS.Timeout | null
  flushing: boolean
}

function isoDay(d: Date): string {
  return d.toISOString().slice(0, 10)
}

function safeJsonLine(obj: unknown): string {
  try {
    return JSON.stringify(obj)
  } catch {
    return JSON.stringify({
      ts: new Date().toISOString(),
      level: 'error',
      scope: 'backend',
      event: 'diagnostics.stringify_failed',
    })
  }
}

async function ensureDir(path: string) {
  await mkdir(path, { recursive: true })
}

export class LocalDiagnostics {
  private readonly baseDir: string
  private retention: RetentionPolicy
  private enabled = true
  private readonly pending = new Map<string, PendingFile>()
  private maintenanceTimer: NodeJS.Timeout | null = null
  private readonly lastInputPromptSigBySession = new Map<string, PromptSig>()

  constructor(opts: { baseDir: string; retention?: Partial<RetentionPolicy>; enabled?: boolean }) {
    this.baseDir = opts.baseDir
    this.retention = {
      maxAgeDays: typeof opts.retention?.maxAgeDays === 'number' ? opts.retention.maxAgeDays : 7,
      maxTotalBytes:
        typeof opts.retention?.maxTotalBytes === 'number'
          ? opts.retention.maxTotalBytes
          : 50 * 1024 * 1024,
    }
    if (typeof opts.enabled === 'boolean') this.enabled = opts.enabled
  }

  setRetention(policy: Partial<RetentionPolicy>) {
    const prev = this.retention
    const next: RetentionPolicy = { ...prev }
    if (typeof policy.maxAgeDays === 'number' && Number.isFinite(policy.maxAgeDays)) {
      next.maxAgeDays = Math.max(1, Math.floor(policy.maxAgeDays))
    }
    if (typeof policy.maxTotalBytes === 'number' && Number.isFinite(policy.maxTotalBytes)) {
      next.maxTotalBytes = Math.max(1024 * 1024, Math.floor(policy.maxTotalBytes))
    }
    const changed = next.maxAgeDays !== prev.maxAgeDays || next.maxTotalBytes !== prev.maxTotalBytes
    this.retention = next
    if (changed) void this.cleanup().catch(() => {})
  }

  getRetention(): RetentionPolicy {
    return { ...this.retention }
  }

  setEnabled(enabled: boolean) {
    this.enabled = Boolean(enabled)
  }

  isEnabled(): boolean {
    return this.enabled
  }

  noteInputPromptSig(sessionId: string, sig: PromptSig) {
    if (!sessionId) return
    this.lastInputPromptSigBySession.set(sessionId, sig)
  }

  getLastInputPromptSig(sessionId: string): PromptSig | null {
    return this.lastInputPromptSigBySession.get(sessionId) || null
  }

  getDiagnosticsDir(): string {
    return join(this.baseDir, 'diagnostics')
  }

  private getAppLogPath(day = isoDay(new Date())): string {
    return join(this.getDiagnosticsDir(), `app-${day}.jsonl`)
  }

  private getSessionLogPath(sessionId: string, day = isoDay(new Date())): string {
    return join(this.getDiagnosticsDir(), 'sessions', `${sessionId}-${day}.jsonl`)
  }

  async startMaintenance() {
    await this.cleanup()
    if (this.maintenanceTimer) clearInterval(this.maintenanceTimer)
    this.maintenanceTimer = setInterval(
      () => {
        void this.cleanup().catch(() => {})
      },
      24 * 60 * 60 * 1000,
    )
  }

  stopMaintenance() {
    if (this.maintenanceTimer) clearInterval(this.maintenanceTimer)
    this.maintenanceTimer = null
  }

  async append(event: DiagnosticsEvent): Promise<void> {
    if (!this.enabled) return
    const day = isoDay(new Date())
    const redacted: DiagnosticsEvent = {
      ...event,
      ts: event.ts || new Date().toISOString(),
      data: redactJson(event.data),
      correlation: redactJson(event.correlation) as any,
    }
    const line = safeJsonLine(redacted) + '\n'

    const appPath = this.getAppLogPath(day)
    await this.queueAppend(appPath, line)

    if (event.sessionId) {
      const sessionPath = this.getSessionLogPath(event.sessionId, day)
      await this.queueAppend(sessionPath, line)
    }
  }

  private async queueAppend(path: string, chunk: string): Promise<void> {
    const existing = this.pending.get(path)
    if (existing) {
      existing.buffer += chunk
      if (existing.buffer.length > 64 * 1024) void this.flushPath(path)
      return
    }

    this.pending.set(path, { buffer: chunk, timer: null, flushing: false })
    const item = this.pending.get(path)!
    item.timer = setTimeout(() => {
      void this.flushPath(path)
    }, 250)
  }

  private async flushPath(path: string): Promise<void> {
    const item = this.pending.get(path)
    if (!item || item.flushing) return
    item.flushing = true
    if (item.timer) clearTimeout(item.timer)
    item.timer = null

    const data = item.buffer
    item.buffer = ''
    try {
      await ensureDir(dirname(path))
      await appendFile(path, data, 'utf8')
    } catch {
      // ignore
    } finally {
      item.flushing = false
      if (!item.buffer) this.pending.delete(path)
      else void this.flushPath(path)
    }
  }

  async cleanup(): Promise<void> {
    const dir = this.getDiagnosticsDir()
    await ensureDir(dir)
    await ensureDir(join(dir, 'sessions'))
    await ensureDir(join(dir, 'bundles'))

    const now = Date.now()
    const maxAgeMs = this.retention.maxAgeDays * 24 * 60 * 60 * 1000

    const files: Array<{ path: string; mtimeMs: number; size: number }> = []

    const scan = async (root: string) => {
      let entries: string[] = []
      try {
        entries = await readdir(root)
      } catch {
        return
      }
      for (const name of entries) {
        const full = join(root, name)
        let s
        try {
          s = await stat(full)
        } catch {
          continue
        }
        if (s.isDirectory()) await scan(full)
        else files.push({ path: full, mtimeMs: s.mtimeMs, size: s.size })
      }
    }

    await scan(dir)

    // Age-based deletion.
    for (const f of files) {
      if (now - f.mtimeMs <= maxAgeMs) continue
      try {
        await unlink(f.path)
      } catch {
        /* ignore */
      }
    }

    // Size-based trimming (oldest first).
    const remaining: Array<{ path: string; mtimeMs: number; size: number }> = []
    let total = 0
    for (const f of files) {
      try {
        const s = await stat(f.path)
        if (!s.isFile()) continue
        remaining.push({ path: f.path, mtimeMs: s.mtimeMs, size: s.size })
        total += s.size
      } catch {
        // ignore
      }
    }
    remaining.sort((a, b) => a.mtimeMs - b.mtimeMs)
    while (total > this.retention.maxTotalBytes && remaining.length) {
      const victim = remaining.shift()!
      total -= victim.size
      try {
        await unlink(victim.path)
      } catch {
        /* ignore */
      }
    }
  }

  async readRecentLogText(params: {
    sessionId?: string | null
    maxBytes: number
  }): Promise<{ app: string; session: string }> {
    const day = isoDay(new Date())
    const appPath = this.getAppLogPath(day)
    const sessionPath = params.sessionId ? this.getSessionLogPath(params.sessionId, day) : ''

    const readTail = async (path: string) => {
      if (!path) return ''
      try {
        const s = await stat(path)
        const size = s.size
        const start = Math.max(0, size - params.maxBytes)
        const buf = await readFile(path)
        const slice = start > 0 ? buf.subarray(start) : buf
        return slice.toString('utf8')
      } catch {
        return ''
      }
    }

    const app = await readTail(appPath)
    const session = await readTail(sessionPath)
    return { app, session }
  }

  async buildExportBundle(params: {
    sessionId?: string | null
    appVersion?: string
    appState?: PersistedAppStateV2 | null
    debugTraceText?: string
  }): Promise<Buffer> {
    const now = new Date()
    const logs = await this.readRecentLogText({
      sessionId: params.sessionId || null,
      maxBytes: 2 * 1024 * 1024,
    })

    const state = params.appState ? (redactJson(params.appState) as any) : null

    const manifest = {
      exportedAt: now.toISOString(),
      platform: platform(),
      baseDir: this.baseDir,
      diagnosticsDir: this.getDiagnosticsDir(),
      sessionId: params.sessionId || null,
      appVersion: params.appVersion || '',
      redaction: 'redacted',
      retention: this.retention,
    }

    const zip = new ZipBuilder()
    zip.addFile('manifest.json', JSON.stringify(manifest, null, 2) + '\n')
    zip.addFile('settings.json', state ? JSON.stringify(state, null, 2) + '\n' : '{}\n')
    zip.addFile('app-log.jsonl', logs.app || '')
    zip.addFile('session-log.jsonl', logs.session || '')
    zip.addFile('debug-trace.txt', redactText(params.debugTraceText || ''))
    return zip.toBuffer()
  }

  async exportToPath(params: {
    outputPath: string
    sessionId?: string | null
    appVersion?: string
    appState?: PersistedAppStateV2 | null
    debugTraceText?: string
  }): Promise<string> {
    const buf = await this.buildExportBundle(params)
    await ensureDir(dirname(params.outputPath))
    await writeFile(params.outputPath, buf)
    return params.outputPath
  }

  computePromptSig(text: string): PromptSig {
    return promptSig(text)
  }

  logPipelineExitcodeRisk(params: {
    sessionId?: string
    command: string
    toolName?: string
    correlation?: Record<string, unknown>
  }) {
    const cmd = String(params.command || '')
    if (!/\\|\\s*tail\\b/.test(cmd)) return
    void this.append({
      ts: new Date().toISOString(),
      level: 'warn',
      scope: 'backend',
      event: 'anomaly.pipeline_exitcode_risk',
      sessionId: params.sessionId,
      correlation: params.correlation,
      data: { toolName: params.toolName || '', commandPreview: makeCommandPreview(cmd) },
    })
  }
}

function makeCommandPreview(cmd: string): string {
  const s = redactText(String(cmd || ''))
  return s.length > 500 ? `${s.slice(0, 500)}...[truncated]` : s
}
