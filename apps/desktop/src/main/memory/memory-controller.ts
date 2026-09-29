// The Desktop Shell's side of Memory: what Settings → Memory shows, the manual
// consolidation, and the automatic extraction the hooks ask for through
// request files (ADR 0011). No Electron here, so the E2E suite runs it as is.
import { mkdirSync, readdirSync, readFileSync, unlinkSync, watch, type FSWatcher } from 'node:fs'
import { join } from 'node:path'
import {
  LIMITS,
  openMemoryStore,
  projectSlot,
  type MemorySlot,
  type MemoryStore,
} from '../../memory/store'
import { REQUESTS_FOLDER, type ExtractionRequest } from '../../memory/hook'
import type { ConsolidationResult, MemoryOverview } from '../../shared/memory'
import type { RunMemorySession } from './memory-session'
import { consolidate, extract } from './memory-work'
import { readPrompt, resetPrompts } from './prompts'

export interface MemoryControllerOptions {
  memoryDir: string
  /** The shipped prompts. */
  defaultsDir: string
  /** Null while there is no Daemon to run a Memory Session on. */
  runner: () => RunMemorySession | null
  modelId: () => string
  /** Something Settings → Memory shows changed. */
  onChange: () => void
  log?: (message: string) => void
}

export interface MemoryController {
  overview(): MemoryOverview
  consolidate(workspace: string | null): Promise<ConsolidationResult>
  resetPrompts(): void
  /** Handles the extraction requests waiting now and every one that arrives until stop(). */
  watchRequests(): void
  /** Handles the waiting requests once, e.g. when the Daemon is back. */
  processRequests(): Promise<void>
  stop(): void
}

const slotKey = (workspace: string | null) => workspace ?? ''

export function createMemoryController(options: MemoryControllerOptions): MemoryController {
  const log = options.log ?? (() => {})
  let store: MemoryStore | null = null
  const open = () => (store ??= openMemoryStore(options.memoryDir))
  const consolidating = new Set<string>()
  const requestsDir = join(options.memoryDir, REQUESTS_FOLDER)
  let watcher: FSWatcher | null = null
  let draining: Promise<void> | null = null
  let again = false

  const work = (prompt: 'consolidation' | 'extraction', run: RunMemorySession) => ({
    store: open(),
    run,
    modelId: options.modelId(),
    prompt: readPrompt(options.memoryDir, options.defaultsDir, prompt),
  })

  const drain = async () => {
    const run = options.runner()
    if (!run) return
    let files: string[]
    try {
      files = readdirSync(requestsDir).filter((f) => f.endsWith('.json'))
    } catch {
      return
    }
    for (const file of files) {
      const path = join(requestsDir, file)
      let request: ExtractionRequest
      try {
        request = JSON.parse(readFileSync(path, 'utf8')) as ExtractionRequest
      } catch {
        unlinkSync(path)
        continue
      }
      try {
        const { added, refused } = await extract(work('extraction', run), request)
        log(`extracted ${added} entries (${refused} refused) from ${request.sessionId}`)
        if (added > 0) options.onChange()
      } catch (error) {
        // One attempt only: a request that keeps failing must not loop forever.
        log(`extraction from ${request.sessionId} failed: ${String(error)}`)
      }
      try {
        unlinkSync(path)
      } catch {
        // Already gone.
      }
    }
  }

  const processRequests = (): Promise<void> => {
    if (draining) {
      again = true
      return draining
    }
    draining = (async () => {
      do {
        again = false
        await drain()
      } while (again)
    })().finally(() => {
      draining = null
    })
    return draining
  }

  return {
    overview() {
      return {
        folder: options.memoryDir,
        rows: open()
          .summaries()
          .map((summary) => ({
            workspace: summary.workspace,
            entries: summary.entries,
            chars: summary.chars,
            softLimit: LIMITS[summary.scope].soft,
            overSoftLimit: summary.overSoftLimit,
            lastConsolidated: summary.lastConsolidated,
            consolidating: consolidating.has(slotKey(summary.workspace)),
          })),
      }
    },
    async consolidate(workspace) {
      const run = options.runner()
      if (!run) throw new Error('The Daemon is not running.')
      const key = slotKey(workspace)
      if (consolidating.has(key)) throw new Error('This Memory is already being consolidated.')
      const slot: MemorySlot = workspace === null ? { scope: 'global' } : projectSlot(workspace)
      consolidating.add(key)
      options.onChange()
      try {
        const outcomes = await consolidate(work('consolidation', run), slot)
        for (const o of outcomes)
          if (!o.ok) log(`consolidation of ${o.category} rejected: ${o.reason}`)
        return {
          applied: outcomes.filter((o) => o.ok).length,
          rejected: outcomes.filter((o) => !o.ok).length,
        }
      } finally {
        consolidating.delete(key)
        options.onChange()
      }
    },
    resetPrompts() {
      resetPrompts(options.memoryDir, options.defaultsDir)
    },
    watchRequests() {
      if (watcher) return
      mkdirSync(requestsDir, { recursive: true })
      watcher = watch(requestsDir, () => void processRequests())
      void processRequests()
    },
    processRequests,
    stop() {
      watcher?.close()
      watcher = null
      store?.close()
      store = null
    },
  }
}
