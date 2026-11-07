import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { Harness, createRegistry } from '@earendil-works/pi-durable'
import type { HarnessSettings, Registry, Storage } from '@earendil-works/pi-durable'
import { SqliteStorage } from '@earendil-works/pi-durable/storage/sqlite'
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node'
import type { MutableModels } from '@earendil-works/pi-ai'
import { type KernelConfig } from './config.ts'
import { codingTools } from './extensions/coding.ts'
import { photonImages } from './images.ts'
import { Indexer } from './indexer/index.ts'
import { info, error } from './log.ts'
import { buildModels } from './models.ts'
import { serve, type ServerHandle } from './protocol/server.ts'
import { BunSqliteDatabase } from './storage/facade.ts'
import { Kernel, context } from './session/service.ts'
import { reconcilePending } from './reconcile.ts'

/** What this build is, for `harness.info` and the handshake. Bumped with a Pi upgrade or our own fix. */
export const VERSION = 'kernel-1.5.0-1'
export const PI_VERSION = '1.5.0'

/** What M1 implements. The rest is M3/M4; `harness.info` says so instead of failing at the call site. */
export const CAPABILITIES: Record<string, boolean> = {
  conversations: true,
  compaction: true,
  fork: true,
  usage: true,
  tasks: true,
  index: true,
  images: true,
  processes: false,
  approvals: false,
  asks: false,
  mcp: false,
  skills: false,
  codemode: false,
  models: false,
}

export type KernelHandle = {
  readonly port: number
  readonly kernel: Kernel
  /** The Pi harness, for diagnostics and for tests that need a commit of their own. */
  readonly harness: Harness
  readonly indexer: Indexer
  readonly server: ServerHandle
  readonly config: KernelConfig
  stop(): Promise<void>
}

export type StartOptions = {
  token: string
  /** For tests: a catalog of their own, such as a faux provider. */
  models?: MutableModels
  /** For tests: skip the single-instance lock, so a test can open many kernels at once. */
  noLock?: boolean
  /** For tests and for a future M1 image measurement: the processor to use for `read`. */
  images?: ReturnType<typeof photonImages>
  platform?: 'darwin' | 'linux' | 'win32'
}

/**
 * Start the kernel: one SQLite, one Pi Harness, one HTTP endpoint, and the conversations on it.
 *
 * The order matters. Storage comes first because the Harness reads through it; the indexer's backfill
 * runs before the endpoint listens, so the first `conversation.list` already sees history; and
 * `resume()` comes last, after `onReport` and the broadcaster exist, so a task that recovers from a
 * crash tells the clients while it does.
 */
export async function startKernel(
  config: KernelConfig,
  options: StartOptions,
): Promise<KernelHandle> {
  mkdirSync(config.dataDir, { recursive: true })
  const lockPath = join(config.dataDir, 'kernel.lock')
  if (options.noLock !== true) takeLock(lockPath)

  const databasePath = join(config.dataDir, 'kernel.sqlite')
  const db = new BunSqliteDatabase(databasePath)
  let storage: Storage | undefined
  let harness: Harness | undefined
  let server: ServerHandle | undefined
  /** Set by `onReport` (or the Session's own close) when the Session failed; turns into an exit below. */
  let failed: unknown
  let kernel: Kernel | undefined
  try {
    storage = await SqliteStorage.open(db, { prefix: 'pi_' })
    const models = options.models ?? buildModels(config.providers)
    const coding = codingTools({
      platform: options.platform,
      images: options.images ?? photonImages(),
    })
    const registry: Registry = createRegistry()
    registry.install(coding.extension)

    const settings: HarnessSettings = {
      extensions: [coding.extension],
      // Our own metadata and search writes are batched by the indexer; Pi's progress commits are its
      // own, and on Windows each costs about 16 ms, so they stay at Pi's defaults, not faster.
      ...(config.progress === undefined ? {} : { progress: config.progress }),
    }

    let indexer: Indexer | undefined
    harness = await Harness.open(
      storage,
      {
        models,
        registry,
        settings,
        // A viewer that another process drives: every commit with writes is refused by Pi itself, so no
        // path in the kernel can slip a write through.
        readOnly: config.readOnly,
        env: (target) =>
          new NodeExecutionEnv({
            cwd: target.cwd ?? process.cwd(),
            ...(coding.shellPath === undefined ? {} : { shellPath: coding.shellPath }),
          }),
        onReport: (report) => {
          const detail = report.error instanceof Error ? report.error.message : String(report.error)
          error('harness report', { kind: report.kind, detail })
          server?.broadcast('warning', { kind: report.kind, message: detail })
          // A Storage error is final: Pi closes the Session, and a kernel that stays up cannot write
          // anything. Say so on the wire, then leave with a non-zero status so the Host restarts us.
          if (report.kind === 'storage_failed') {
            // Mark it at once, before the request that hit the failure is answered: the client's answer
            // should say the kernel failed, not that the method has a bug.
            failed = report.error
            kernel?.fail(report.error)
          }
        },
      },
      context,
    )

    indexer = new Indexer(db, storage, {
      // A flush that could not be stored is reported, not dropped: a silent failure here is an index that
      // quietly stops keeping up.
      onError: (cause) => {
        error('the index flush failed', {
          detail: cause instanceof Error ? cause.message : String(cause),
        })
        server?.broadcast('warning', {
          kind: 'index_failed',
          message: cause instanceof Error ? cause.message : String(cause),
        })
      },
      // A flush that changed a conversation tells every client, so the list and the titles on other
      // screens move as the conversation does.
      onChange: (ids) => {
        for (const id of ids) {
          void indexer!
            .conversation(id)
            .then((row) => {
              if (row === undefined) return
              server?.broadcast('index.changed', {
                id,
                ...(row.title === null ? {} : { title: row.title }),
                archived: row.archived_at !== null,
                lastActivityAt: row.last_activity_at,
              })
            })
            .catch(() => undefined)
        }
      },
    })
    await indexer.open()
    harness.observeCommits(indexer.observeCommits())
    // The first run indexes everything; every later start re-reads what a crash left behind the watermark.
    const caughtUp = await indexer.catchUp()
    if (caughtUp > 0) info('index caught up', { entries: caughtUp, watermark: indexer.watermark })

    kernel = new Kernel({
      config,
      db,
      storage,
      harness,
      indexer,
      models,
      registry,
      tools: coding.names,
      version: VERSION,
      piVersion: PI_VERSION,
      capabilities: CAPABILITIES,
      shutdown: () => stop(),
      onFailed: (cause) => {
        failed = cause
      },
    })
    // A conversation that was live when the process died must tell clients about its run as it resumes.
    for (const task of (await harness.inspect(context)).tasks) {
      await kernel!.track(task.record.conversationId as unknown as number)
    }

    // After a crash, close the questions the dead process left waiting before anything resumes. A
    // read-only viewer writes nothing, so it leaves them as it found them.
    if (!config.readOnly) await reconcilePending(harness, context)

    server = serve({
      host: config.host,
      port: config.port,
      token: options.token,
      dispatch: kernel,
      health: () => ({ version: VERSION, pi: PI_VERSION }),
      onConnectionClosed: (connectionId) => kernel?.releaseConnection(connectionId),
    })
    kernel.attach(server)
    await kernel.observeTasks()
    if (config.readOnly) {
      // A viewer sees what another process drives. Resuming here would run its tasks a second time.
      info('kernel listening read-only', { port: server.port, data: config.dataDir })
    } else {
      harness.resume()
    }
    info('kernel listening', { port: server.port, data: config.dataDir, readOnly: config.readOnly })

    // A failed Session is fatal: the kernel stops answering (every request is `-32010`), closes what it
    // can, and exits non-zero so the Supervisor replaces it. `Session.closed` is the authoritative signal
    // — `onReport` may describe the same event, or an earlier symptom of it. Watching a flag rather than
    // throwing from a Pi callback matters too: a callback has nowhere to report.
    void harness.closed.then((end) => {
      if (end.kind === 'failed') failed = end.error
    })
    const failedWatch = setInterval(() => {
      if (failed === undefined) return
      clearInterval(failedWatch)
      error('the Session failed; exiting so the Host can restart the kernel', {
        reason: failed instanceof Error ? failed.message : String(failed),
      })
      // Mark it first, so a request that arrives in the next moment is answered `-32010` instead of
      // finding a process that is up but cannot write. The grace lets that answer reach the socket.
      kernel?.fail(failed)
      setTimeout(() => {
        void stop().finally(() => process.exit(3))
      }, 500)
    }, 50)
    failedWatch.unref?.()

    const closing = { done: false }
    const stop = async (): Promise<void> => {
      if (closing.done) return
      closing.done = true
      await kernel?.close()
      await server?.stop()
      // Pi's `harness.close()` seals admission and settles its own writes; the Storage keeps the file
      // open until it is closed too, and only then may the database close.
      clearInterval(failedWatch)
      // A failed Session already closed itself; closing again is a no-op, not an error.
      await harness?.close(context).catch(() => undefined)
      await storage?.close(context).catch(() => undefined)
      await db.close().catch(() => undefined)
      if (options.noLock !== true) {
        try {
          rmSync(lockPath)
        } catch {
          // A lock left behind is harmless: the next start checks whether its process is alive.
        }
      }
    }
    return {
      get port() {
        return server!.port
      },
      kernel,
      server,
      config,
      get harness() {
        return harness!
      },
      get indexer() {
        return indexer!
      },
      stop,
    }
  } catch (cause) {
    await harness?.close(context).catch(() => undefined)
    await storage?.close(context).catch(() => undefined)
    await db.close().catch(() => undefined)
    await server?.stop().catch(() => undefined)
    throw cause
  }
}

/**
 * One kernel per data directory: a second one would open a second connection to the same SQLite file,
 * and a synchronous `busy_timeout` wait there blocks the whole event loop.
 */
function takeLock(path: string): void {
  // Exclusive create: two kernels starting at once cannot both win.
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      writeFileSync(path, String(process.pid), { flag: 'wx' })
      return
    } catch (cause) {
      if ((cause as { code?: string }).code !== 'EEXIST') throw cause
    }
    let existing: number
    try {
      existing = Number(readFileSync(path, 'utf8').trim())
    } catch {
      // Gone between the two calls: try the exclusive create again.
      continue
    }
    if (Number.isInteger(existing) && existing > 0 && isAlive(existing)) {
      throw new Error(`another kernel (pid ${existing}) is using this data directory`)
    }
    // A lock from a process that is gone. It is **claimed by rename**, not deleted: `rename` is atomic, so
    // of two starters that both saw the same dead PID only one wins the rename, and the loser's retry sees
    // the winner's fresh lock. Deleting would let both proceed and one could delete the other's lock.
    const claimed = `${path}.${process.pid}.stale`
    try {
      renameSync(path, claimed)
    } catch {
      // Somebody else claimed it first; try to create our own again.
      continue
    }
    // The claim may have taken a live lock that its owner wrote between our read and our rename.
    try {
      const stolen = Number(readFileSync(claimed, 'utf8').trim())
      if (Number.isInteger(stolen) && stolen > 0 && stolen !== existing && isAlive(stolen)) {
        renameSync(claimed, path)
        throw new Error(`another kernel (pid ${stolen}) is using this data directory`)
      }
    } finally {
      rmSync(claimed, { force: true })
    }
  }
  throw new Error('could not take the data directory lock')
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}
