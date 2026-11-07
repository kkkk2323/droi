import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
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
        env: (target) =>
          new NodeExecutionEnv({
            cwd: target.cwd ?? process.cwd(),
            ...(coding.shellPath === undefined ? {} : { shellPath: coding.shellPath }),
          }),
        onReport: (report) => {
          const detail = report.error instanceof Error ? report.error.message : String(report.error)
          error('harness report', { kind: report.kind, detail })
          server?.broadcast('warning', { kind: report.kind, message: detail })
        },
      },
      context,
    )

    indexer = new Indexer(db, storage)
    await indexer.open()
    harness.observeCommits(indexer.observeCommits())
    await indexer.backfill()

    const kernel = new Kernel({
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
    })
    // A conversation that was live when the process died must tell clients about its run as it resumes.
    for (const task of (await harness.inspect(context)).tasks) {
      await kernel.track(task.record.conversationId as unknown as number)
    }

    // After a crash, close the questions the dead process left waiting before anything resumes.
    await reconcilePending(harness, context)

    server = serve({
      host: config.host,
      port: config.port,
      token: options.token,
      dispatch: kernel,
      health: () => ({ version: VERSION, pi: PI_VERSION }),
    })
    kernel.attach(server)
    harness.resume()
    info('kernel listening', { port: server.port, data: config.dataDir, readOnly: config.readOnly })

    const closing = { done: false }
    const stop = async (): Promise<void> => {
      if (closing.done) return
      closing.done = true
      await kernel.close()
      await server?.stop()
      // Pi's `harness.close()` seals admission and settles its own writes; the Storage keeps the file
      // open until it is closed too, and only then may the database close.
      await harness?.close(context)
      await storage?.close(context)
      await db.close()
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
  try {
    const existing = Number(readFileSync(path, 'utf8').trim())
    if (Number.isInteger(existing) && existing > 0 && isAlive(existing)) {
      throw new Error(`another kernel (pid ${existing}) is using this data directory`)
    }
  } catch (cause) {
    if (cause instanceof Error && cause.message.startsWith('another kernel')) throw cause
    // No lock, or an unreadable one: either way this process may take it.
  }
  writeFileSync(path, String(process.pid))
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}
