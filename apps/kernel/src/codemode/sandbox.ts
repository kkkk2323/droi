import {
  CodemodeSandbox,
  loadQuickJSWasm,
  type CodemodeRun,
  type CodemodeRunOptions,
} from '@earendil-works/pi-codemode'
import wasmPath from '../vendor/quickjs.wasm' with { type: 'file' }

/**
 * How long a script may run when its `// @options:` line names no `timeout_ms`. Pi's durable start has no limit of
 * its own, so a script that never ends would hold its worker thread until the user stops the turn, and an
 * Automation has no user to. Long enough for a script that waits on a full build or test run.
 *
 * Unlike a `timeout_ms` the script names, this one is not stored with the run: a script resumed after a restart gets
 * it afresh.
 */
export const DEFAULT_SCRIPT_TIMEOUT_MS = 30 * 60_000

/**
 * The sandbox codemode scripts run in: QuickJS (`vendor/quickjs.wasm`, MIT, see `vendor/NOTICE.md`) in a worker
 * thread of its own per script, so a script that computes never holds up the Kernel's event loop, and one that
 * spins is stopped by its timeout.
 *
 * Both files have to be found inside a single-file `bun build --compile` binary, where no package is on disk.
 * The wasm comes the way Photon's does (`import … with { type: 'file' }`). The worker is an entrypoint of the
 * build of its own (`build.ts`), which such a binary starts only by the bare relative specifier it was built
 * from; a `new URL(…, import.meta.url)` names a file the binary does not have (oven-sh/bun#29124). From source
 * the opposite holds: a bare specifier is resolved against the working directory, so it is a URL there.
 */
export function codemodeSandbox(): CodemodeSandbox {
  // The sandbox awaits `wasm` when a script starts; a thenable defers compiling QuickJS to the first script,
  // so a kernel that never runs one never pays for it. `loadQuickJSWasm` compiles once per path.
  const wasm = {
    then: (resolve: (module: object) => unknown, reject: (cause: unknown) => unknown) =>
      loadQuickJSWasm(wasmPath).then(resolve, reject),
  } as unknown as Promise<object>
  return new TimedSandbox({ wasm, workerUrl: workerUrl() })
}

class TimedSandbox extends CodemodeSandbox {
  override start(code: string, options: CodemodeRunOptions): CodemodeRun {
    return super.start(code, {
      ...options,
      timeoutMs: options.timeoutMs ?? DEFAULT_SCRIPT_TIMEOUT_MS,
    })
  }
}

function workerUrl(): string | URL {
  const compiled = import.meta.url.includes('$bunfs') || import.meta.url.includes('~BUN')
  return compiled ? './codemode-worker.ts' : new URL('../codemode-worker.ts', import.meta.url)
}
