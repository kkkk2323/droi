import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { Harness, createRegistry, defineExtension, section } from '@earendil-works/pi-durable'
import type { Extension, HarnessSettings, Registry, Storage } from '@earendil-works/pi-durable'
import { SqliteStorage } from '@earendil-works/pi-durable/storage/sqlite'
import { createToolProcesses, type ToolProcesses } from './tool-processes.ts'
import type { MutableModels } from '@earendil-works/pi-ai'
import { type KernelConfig } from './config.ts'
import { codingTools, shellChoice } from './extensions/coding.ts'
import { approvalExtension } from './extensions/approval.ts'
import { Approvals } from './policy/approvals.ts'
import { askExtension } from './extensions/ask.ts'
import { Asks } from './ask/asks.ts'
import { backgroundExtension } from './extensions/background.ts'
import { Background } from './processes/background.ts'
import { McpServers } from './mcp/servers.ts'
import type { CodemodeNamespace } from '@earendil-works/pi-durable/codemode'
import { codemodeExtension, codemodeInMode, codemodeKey } from './extensions/codemode.ts'
import { compactionModelExtension } from './extensions/compaction-model.ts'
import { memoryExtension } from './extensions/memory.ts'
import { codemodeSandbox } from './codemode/sandbox.ts'
import { McpAuth } from './mcp/oauth.ts'
import { PolicyStore } from './policy/store.ts'
import { resourcesExtension } from './extensions/resources.ts'
import { DisabledSkills } from './resources/disabled.ts'
import { photonImages } from './images.ts'
import { Indexer } from './indexer/index.ts'
import { repoRootOf } from './workspace.ts'
import { info, error, warn } from './log.ts'
import { buildModels } from './models.ts'
import { serve, type ServerHandle } from './protocol/server.ts'
import { BunSqliteDatabase } from './storage/facade.ts'
import { Kernel, context } from './session/service.ts'
import { takeLock, type Lock } from './lock.ts'
import { reconcilePending } from './reconcile.ts'

/** What this build is, for `harness.info` and the handshake. Bumped with a Pi upgrade or our own fix. */
export const VERSION = 'kernel-1.5.0-1'
/** The extension that carries the brand's app's section (ADR 0041). */
export const APP_EXTENSION = 'droi-app'
export const PI_VERSION = '1.5.0'

/** What M1 implements. The rest is M3/M4; `harness.info` says so instead of failing at the call site. */
export const CAPABILITIES: Record<string, boolean> = {
  conversations: true,
  compaction: true,
  fork: true,
  usage: true,
  tasks: true,
  index: true,
  import: true,
  images: true,
  processes: true,
  approvals: true,
  asks: true,
  mcp: true,
  skills: true,
  codemode: true,
  resources: true,
  models: true,
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
  /** For tests: extensions installed beside the coding one, such as a tool that makes a nested call. */
  extensions?: Extension[]
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
  // The lock is a database of its own, not a file of text: the operating system enforces it and releases it
  // when this process ends, however it ends.
  const lockPath = join(config.dataDir, 'kernel.lock.db')
  const lock: Lock | undefined = options.noLock === true ? undefined : takeLock(lockPath)

  const databasePath = join(config.dataDir, 'kernel.sqlite')
  const db = new BunSqliteDatabase(databasePath)
  let storage: Storage | undefined
  let harness: Harness | undefined
  let server: ServerHandle | undefined
  /** Set by `onReport` (or the Session's own close) when the Session failed; turns into an exit below. */
  let failed: unknown
  let kernel: Kernel | undefined
  /** Every tool shell this Kernel started, so closing it can end what a tool left running. */
  let tools: ToolProcesses | undefined
  /** The MCP servers, whose processes a failed start must not leave running. */
  let mcp: McpServers | undefined
  try {
    storage = await SqliteStorage.open(db, { prefix: 'pi_' })
    const models = options.models ?? buildModels(config.providers)
    const coding = codingTools({
      platform: options.platform,
      images: options.images ?? photonImages(),
    })
    // Made once the harness is open and before it resumes, so no call runs unjudged.
    let approvals: Approvals | undefined
    const approvalHook = approvalExtension(() => approvals)
    // Read after the index has migrated the database, before anything renders a prompt: `resume()` comes later.
    let disabledSkills: DisabledSkills | undefined
    const resources = resourcesExtension({
      userDir: config.userDir,
      disabled: () => disabledSkills?.names() ?? new Set(),
    })
    let asks: Asks | undefined
    const askTool = askExtension(() => asks)
    let background: Background | undefined
    const backgroundTool = backgroundExtension(() => background)
    /** The MCP servers as codemode groups their tools: `McpServers` keeps the list current. */
    const namespaces: CodemodeNamespace[] = []
    const codemode = codemodeExtension({
      sandbox: codemodeSandbox(),
      namespaces,
      // Read once the harness is open: a background script is stopped as Harness.abortTask stops a task.
      abortTask: async (id) => {
        await harness?.abortTask(id as never, context)
      },
    })
    // Always installed, and selected per conversation before each turn while Memory is on (selectMemory), so
    // harness.configure can turn it on or off without a restart. The folder is read as config holds it then.
    const memory = memoryExtension({
      dir: () => config.memoryDir,
      onRead: (file, at) => server?.broadcast('memory.read', { file, at: at.toISOString() }),
      history: () => indexer,
    })
    // The brand's app's section (ADR 0041), read at each turn so harness.configure can change it.
    const appSection = defineExtension({
      name: APP_EXTENSION,
      sections: [section(APP_EXTENSION, () => config.app?.section ?? '', { tag: false })],
    })
    const registry: Registry = createRegistry()
    registry.install(coding.extension)
    registry.install(appSection)
    registry.install(approvalHook)
    registry.install(resources)
    registry.install(askTool)
    registry.install(backgroundTool)
    registry.install(codemode)
    registry.install(memory)
    // The compaction model, read at each compaction so harness.configure can change it.
    const compactionModel = compactionModelExtension({
      model: () =>
        config.compactionModel === undefined
          ? undefined
          : {
              ...config.compactionModel,
              ...(config.compactionThinking === undefined
                ? {}
                : { thinkingLevel: config.compactionThinking }),
            },
      warn: (message) => warn('compaction', { message }),
    })
    registry.install(compactionModel)
    for (const extension of options.extensions ?? []) registry.install(extension)
    // Codemode as config says now: in `only` its wraps hide each tool a script can call from the model. They name the
    // tools installed, so every change of the registry (a server that connects) makes them again; one that hides the
    // same tools installs nothing, which is also what ends the change its own install publishes.
    let codemodeShown = ''
    const syncCodemode = (): void => {
      const next = codemodeInMode(codemode, registry.snapshot(), config.codemodeMode ?? 'on')
      const key = `${config.codemodeMode ?? 'on'}\n${codemodeKey(next)}`
      if (key === codemodeShown) return
      codemodeShown = key
      registry.install(next)
    }
    registry.subscribe(syncCodemode)
    syncCodemode()

    // A tool shell runs detached, outside the Kernel's own process group: this is what remembers it.
    tools = createToolProcesses({
      ...(coding.shellPath === undefined ? {} : { shellPath: coding.shellPath }),
      cwd: process.cwd(),
    })

    const settings: HarnessSettings = {
      extensions: [
        coding.extension,
        approvalHook,
        resources,
        askTool,
        backgroundTool,
        codemode,
        appSection,
        compactionModel,
        ...(options.extensions ?? []),
      ],
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
        env: tools.env,
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
      // Pi's entries, for the one repair that has to read an entry's JSON: a lost import-key row.
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
                ...(row.title_source === null ? {} : { titleSource: row.title_source }),
                archived: row.archived_at !== null,
                lastActivityAt: row.last_activity_at,
                viewedAt: row.viewed_at,
              })
            })
            .catch(() => undefined)
        }
      },
    })
    await indexer.open()
    disabledSkills = await DisabledSkills.open(db)
    harness.observeCommits(indexer.observeCommits())
    // The first run indexes everything; every later start re-reads what a crash left behind the watermark.
    const caughtUp = await indexer.catchUp()
    if (caughtUp > 0) info('index caught up', { entries: caughtUp, watermark: indexer.watermark })

    const policy = new PolicyStore({
      ...(config.app === undefined ? {} : { appAllows: config.app.alwaysAllow }),
      ...(config.policy === undefined ? {} : { initial: config.policy }),
      ...(config.policyWaitMs === undefined ? {} : { waitMs: config.policyWaitMs }),
      onChanged: (id, snapshot) => server?.broadcast('policy.changed', { id, policy: snapshot }),
    })
    const opened = harness
    approvals = new Approvals({
      harness,
      policy,
      notify: (method, payload) => server?.broadcast(method, payload),
      cwdOf: async (id) => {
        const conversation = await opened.conversation(id as never, context)
        return conversation === undefined ? undefined : (await conversation.agent(context)).cwd
      },
      ...(config.approvalTimeoutMs === undefined ? {} : { timeoutMs: config.approvalTimeoutMs }),
      ...(options.platform === undefined ? {} : { platform: options.platform }),
      context,
    })

    mcp = new McpServers({
      userDir: config.userDir,
      db,
      registry,
      notify: (method, payload) => server?.broadcast(method, payload),
      authProvider: (name, url) => mcpAuth.authProvider(name, url),
      namespaces,
      version: VERSION,
      context,
      ...(config.app === undefined
        ? {}
        : { app: { servers: config.app.mcpServers, blockOnLoad: config.app.blockOnLoad } }),
    })
    const servers = mcp
    const mcpAuth = new McpAuth(config.dataDir, (key) => servers.reconnect(key))
    // A viewer starts no servers; the process that drives the data does.
    if (!config.readOnly) await mcp.open()

    asks = new Asks({
      harness,
      db,
      notify: (method, payload) => server?.broadcast(method, payload),
      submitted: async (id) => {
        await kernel?.track(id)
        await kernel?.refreshBusy(id)
      },
      ...(config.approvalTimeoutMs === undefined ? {} : { timeoutMs: config.approvalTimeoutMs }),
      context,
    })
    // A read-only viewer lists no waiting cards (`conversation.open` leaves them to the process that drives
    // the conversation), so it has no questions to take back.
    if (!config.readOnly) await asks.open()

    background = new Background({
      harness,
      db,
      dataDir: config.dataDir,
      tools,
      shell: shellChoice({
        ...(options.platform === undefined ? {} : { platform: options.platform }),
      }),
      platform: options.platform ?? process.platform,
      notify: (method, payload) => server?.broadcast(method, payload),
      context,
    })
    // A read-only viewer starts nothing and settles nothing: the process that drives the data does.
    if (!config.readOnly) await background.open()

    kernel = new Kernel({
      config,
      policy,
      approvals,
      asks,
      background,
      mcp,
      mcpAuth,
      db,
      storage,
      harness,
      indexer,
      models,
      registry,
      disabledSkills,
      tools: [...coding.names, 'bash_background'],
      syncCodemode,
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
    if (!config.readOnly) await reconcilePending(harness, context, asks.openIds())

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

    // A conversation's repository is worked out once and stored, so a list never launches git. Rows
    // written before this build have none, a rebuilt index recomputed them from the workspace, and a
    // directory git could not be asked about last time is asked again. After listening: it runs git.
    const backfill = config.readOnly
      ? Promise.resolve()
      : indexer.backfillRepoRoots(repoRootOf).catch((cause) => {
          error('the repository backfill failed', {
            detail: cause instanceof Error ? cause.message : String(cause),
          })
        })

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

    // One stop, shared. A second caller — `kernel.shutdown` on the wire and then SIGINT, or the failure
    // watchdog and a signal — must wait for the same cleanup, not return at once and let a signal handler
    // exit the process in the middle of it. That early exit is how a background tool process used to survive
    // a shutdown that arrived while another shutdown was already running.
    let stopping: Promise<void> | undefined
    const stop = (): Promise<void> => {
      if (stopping !== undefined) return stopping
      stopping = (async () => {
        await kernel?.close()
        await server?.stop()
        await backfill
        // Pi's `harness.close()` seals admission and settles its own writes; the Storage keeps the file
        // open until it is closed too, and only then may the database close.
        clearInterval(failedWatch)
        // Before the harness, which closes the database with it: what waits for the next flush is written now
        // rather than rebuilt by the next start's catch-up. A commit the harness still settles is past the
        // watermark, and that catch-up reads it.
        await indexer?.close().catch(() => undefined)
        // A failed Session already closed itself; closing again is a no-op, not an error.
        await harness?.close(context).catch(() => undefined)
        // After the harness, so no tool starts while this runs; before the storage, so a shutdown leaves no
        // process of ours running in the data directory.
        await tools?.stop(context).catch(() => undefined)
        await storage?.close(context).catch(() => undefined)
        await db.close().catch(() => undefined)
        lock?.release()
      })()
      return stopping
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
    // A start that failed after taking the lock must not hold it: the caller may retry in this process.
    lock?.release()
    await mcp?.close().catch(() => undefined)
    await tools?.stop(context).catch(() => undefined)
    await harness?.close(context).catch(() => undefined)
    await storage?.close(context).catch(() => undefined)
    await db.close().catch(() => undefined)
    await server?.stop().catch(() => undefined)
    throw cause
  }
}
