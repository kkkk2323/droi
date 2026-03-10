import { watch, type FSWatcher } from 'fs'
import { stat } from 'fs/promises'
import { dirname, join } from 'path'
import { readMissionDirSnapshot } from './missionDirReader.ts'
import type {
  MissionDirChangeEvent,
  MissionDirChangeSource,
  MissionDirSnapshot,
} from './missionTypes.ts'
import { MISSION_HANDOFFS_DIR } from './missionTypes.ts'

async function isDirectory(path: string): Promise<boolean> {
  try {
    return (await stat(path)).isDirectory()
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

function getFileSignatures(snapshot: MissionDirSnapshot): Map<string, string> {
  const signatures = new Map<string, string>()
  if (snapshot.state) signatures.set('state.json', stableStringify(snapshot.state))
  if (snapshot.features) signatures.set('features.json', stableStringify(snapshot.features))
  if (snapshot.progressEntries.length > 0) {
    signatures.set('progress_log.jsonl', stableStringify(snapshot.progressEntries))
  }
  if (snapshot.validationState) {
    signatures.set('validation-state.json', stableStringify(snapshot.validationState))
  }
  for (const handoff of snapshot.handoffs) {
    signatures.set(`handoffs/${handoff.fileName}`, stableStringify(handoff.payload))
  }
  return signatures
}

function diffSnapshots(previous: MissionDirSnapshot | null, next: MissionDirSnapshot): string[] {
  if (!previous) {
    return Array.from(getFileSignatures(next).keys())
  }

  const previousSignatures = getFileSignatures(previous)
  const nextSignatures = getFileSignatures(next)
  const changed = new Set<string>()
  for (const [path, signature] of nextSignatures.entries()) {
    if (previousSignatures.get(path) !== signature) changed.add(path)
  }
  for (const path of previousSignatures.keys()) {
    if (!nextSignatures.has(path)) changed.add(path)
  }
  return Array.from(changed).sort()
}

export class MissionDirWatcher {
  private readonly sessionId: string
  private readonly missionDir: string
  private readonly pollIntervalMs: number
  private readonly onChange: (event: MissionDirChangeEvent) => void
  private readonly isDirectory: (path: string) => Promise<boolean>
  private readonly readSnapshot: (missionDir: string) => Promise<MissionDirSnapshot>
  private readonly watchPath: (path: string, listener: () => void) => FSWatcher
  private parentWatcher: FSWatcher | null = null
  private missionDirWatcher: FSWatcher | null = null
  private handoffsWatcher: FSWatcher | null = null
  private pollTimer: NodeJS.Timeout | null = null
  private lastSnapshot: MissionDirSnapshot | null = null
  private syncPromise: Promise<void> | null = null
  private syncQueued = false
  private stopped = false

  constructor(opts: {
    sessionId: string
    missionDir: string
    pollIntervalMs?: number
    onChange: (event: MissionDirChangeEvent) => void
    isDirectory?: (path: string) => Promise<boolean>
    readSnapshot?: (missionDir: string) => Promise<MissionDirSnapshot>
    watchPath?: (path: string, listener: () => void) => FSWatcher
  }) {
    this.sessionId = String(opts.sessionId || '').trim()
    this.missionDir = String(opts.missionDir || '').trim()
    this.pollIntervalMs = Math.max(25, Math.floor(opts.pollIntervalMs || 2000))
    this.onChange = opts.onChange
    this.isDirectory = opts.isDirectory || isDirectory
    this.readSnapshot = opts.readSnapshot || readMissionDirSnapshot
    this.watchPath = opts.watchPath || ((path, listener) => watch(path, listener))
  }

  async start(): Promise<void> {
    if (!this.sessionId) throw new Error('Missing sessionId')
    if (!this.missionDir) throw new Error('Missing missionDir')
    this.stopped = false
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
    this.missionDirWatcher?.close()
    this.missionDirWatcher = null
    this.handoffsWatcher?.close()
    this.handoffsWatcher = null
    try {
      await this.syncPromise
    } catch {
      // Ignore snapshot/read errors during teardown; the next start will re-read from disk.
    }
  }

  private async refreshWatchers(): Promise<void> {
    if (this.stopped) return

    const missionDirExists = await this.isDirectory(this.missionDir)
    if (this.stopped) return
    if (!missionDirExists) {
      this.missionDirWatcher?.close()
      this.missionDirWatcher = null
      this.handoffsWatcher?.close()
      this.handoffsWatcher = null

      const parentDir = dirname(this.missionDir)
      if (!this.parentWatcher && (await this.isDirectory(parentDir))) {
        if (this.stopped) return
        this.parentWatcher = this.watchPath(parentDir, () => {
          void this.sync('fs-watch')
        })
      }
      return
    }

    this.parentWatcher?.close()
    this.parentWatcher = null

    if (!this.missionDirWatcher) {
      this.missionDirWatcher = this.watchPath(this.missionDir, () => {
        void this.sync('fs-watch')
      })
    }

    const handoffsDir = join(this.missionDir, MISSION_HANDOFFS_DIR)
    const handoffsExists = await this.isDirectory(handoffsDir)
    if (this.stopped) return
    if (handoffsExists && !this.handoffsWatcher) {
      this.handoffsWatcher = this.watchPath(handoffsDir, () => {
        void this.sync('fs-watch')
      })
    }
    if (!handoffsExists && this.handoffsWatcher) {
      this.handoffsWatcher.close()
      this.handoffsWatcher = null
    }
  }

  private async sync(source: MissionDirChangeSource): Promise<void> {
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
        const snapshot = await this.readSnapshot(this.missionDir)
        if (this.stopped) return
        const changedPaths = diffSnapshots(this.lastSnapshot, snapshot)
        if (this.stopped) return
        this.lastSnapshot = snapshot
        if (changedPaths.length > 0) {
          this.onChange({
            sessionId: this.sessionId,
            missionDir: snapshot.missionDir,
            changedPaths,
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
