import { watch, type FSWatcher } from 'fs'
import { stat } from 'fs/promises'
import { dirname } from 'path'
import type {
  MissionRuntimeChangeEvent,
  MissionRuntimeChangeSource,
  MissionRuntimeRequest,
  MissionRuntimeSnapshot,
} from './missionTypes.ts'
import { readMissionRuntimeSnapshot } from './workerRuntimeReader.ts'

async function pathExists(path: string): Promise<boolean> {
  try {
    return (await stat(path)).isFile()
  } catch {
    return false
  }
}

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map((entry) => stableStringify(entry)).join(',')}]`
  if (value && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) =>
      a.localeCompare(b),
    )
    return `{${entries
      .map(([key, entry]) => `${JSON.stringify(key)}:${stableStringify(entry)}`)
      .join(',')}}`
  }
  return JSON.stringify(value)
}

function runtimeSignature(snapshot: MissionRuntimeSnapshot): string {
  return stableStringify({
    workerSessionId: snapshot.workerSessionId,
    status: snapshot.status,
    source: snapshot.source,
    message: snapshot.message,
    entries: snapshot.entries,
  })
}

export class WorkerRuntimeWatcher {
  private readonly request: MissionRuntimeRequest
  private readonly pollIntervalMs: number
  private readonly onChange: (event: MissionRuntimeChangeEvent) => void
  private readonly readSnapshot: (params: MissionRuntimeRequest) => Promise<MissionRuntimeSnapshot>
  private readonly pathExists: (path: string) => Promise<boolean>
  private readonly watchPath: (path: string, listener: () => void) => FSWatcher
  private parentWatcher: FSWatcher | null = null
  private fileWatcher: FSWatcher | null = null
  private pollTimer: NodeJS.Timeout | null = null
  private syncPromise: Promise<void> | null = null
  private syncQueued = false
  private stopped = false
  private lastSnapshot: MissionRuntimeSnapshot | null = null
  private lastSignature = ''

  constructor(opts: {
    request: MissionRuntimeRequest
    pollIntervalMs?: number
    onChange: (event: MissionRuntimeChangeEvent) => void
    readSnapshot?: (params: MissionRuntimeRequest) => Promise<MissionRuntimeSnapshot>
    pathExists?: (path: string) => Promise<boolean>
    watchPath?: (path: string, listener: () => void) => FSWatcher
  }) {
    this.request = opts.request
    this.pollIntervalMs = Math.max(25, Math.floor(opts.pollIntervalMs || 2000))
    this.onChange = opts.onChange
    this.readSnapshot = opts.readSnapshot || readMissionRuntimeSnapshot
    this.pathExists = opts.pathExists || pathExists
    this.watchPath = opts.watchPath || ((path, listener) => watch(path, listener))
  }

  async start(): Promise<void> {
    if (!this.request.sessionId) throw new Error('Missing sessionId')
    await this.refreshWatchers()
    await this.sync('initial')
    this.pollTimer = setInterval(() => {
      void this.sync('poll')
    }, this.pollIntervalMs)
    this.pollTimer.unref?.()
  }

  async stop(): Promise<void> {
    this.stopped = true
    if (this.pollTimer) {
      clearInterval(this.pollTimer)
      this.pollTimer = null
    }
    this.parentWatcher?.close()
    this.parentWatcher = null
    this.fileWatcher?.close()
    this.fileWatcher = null
    try {
      await this.syncPromise
    } catch {
      // Ignore teardown races.
    }
  }

  private async refreshWatchers(): Promise<void> {
    if (this.stopped) return
    const snapshot = await this.readSnapshot(this.request)
    if (this.stopped) return
    const sessionFile = String(snapshot.sessionFile || '').trim()
    if (!sessionFile) {
      this.fileWatcher?.close()
      this.fileWatcher = null
      this.parentWatcher?.close()
      this.parentWatcher = null
      return
    }

    const parentDir = dirname(sessionFile)
    if (!this.parentWatcher) {
      this.parentWatcher = this.watchPath(parentDir, () => {
        void this.sync('fs-watch')
      })
    }

    const fileExists = await this.pathExists(sessionFile)
    if (this.stopped) return
    if (fileExists && !this.fileWatcher) {
      this.fileWatcher = this.watchPath(sessionFile, () => {
        void this.sync('fs-watch')
      })
    }
    if (!fileExists && this.fileWatcher) {
      this.fileWatcher.close()
      this.fileWatcher = null
    }
  }

  private async sync(source: MissionRuntimeChangeSource): Promise<void> {
    if (this.stopped) return
    if (this.syncPromise) {
      this.syncQueued = true
      return this.syncPromise
    }

    this.syncPromise = (async () => {
      do {
        this.syncQueued = false
        await this.refreshWatchers()
        if (this.stopped) return
        const snapshot = await this.readSnapshot(this.request)
        if (this.stopped) return
        const signature = runtimeSignature(snapshot)
        const changed = !this.lastSnapshot || signature !== this.lastSignature
        this.lastSnapshot = snapshot
        this.lastSignature = signature
        if (changed) {
          this.onChange({
            sessionId: this.request.sessionId,
            workerSessionId: snapshot.workerSessionId,
            changedPaths: snapshot.sessionFile ? [snapshot.sessionFile] : [],
            source,
            snapshot,
          })
        }
      } while (this.syncQueued && !this.stopped)
    })().finally(() => {
      this.syncPromise = null
    })

    return this.syncPromise
  }
}
