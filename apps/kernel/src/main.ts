import { loadConfig, parseArgs } from './config.ts'
import { startKernel, VERSION } from './kernel.ts'
import { LockError } from './lock.ts'
import { error, info } from './log.ts'

/**
 * The kernel process. It reads its arguments, prints the handshake, and serves until the Host stops it.
 *
 * stdout carries exactly one line — the handshake — because the Host reads it to learn the port. Everything
 * else goes to stderr as JSON lines.
 */
async function main(argv: readonly string[]): Promise<number> {
  // Status `1` is "this start cannot succeed", and status `2` is reserved for the one case a Supervisor may
  // retry — another Kernel holding the data directory. Bad arguments and a missing token are the first kind:
  // starting again the same way fails the same way.
  let config
  try {
    config = loadConfig(parseArgs(argv))
  } catch (cause) {
    process.stderr.write(`${cause instanceof Error ? cause.message : String(cause)}\n`)
    return 1
  }
  const token = process.env['DROI_KERNEL_TOKEN']
  if (token === undefined || token === '') {
    process.stderr.write(
      "DROI_KERNEL_TOKEN is required: the token travels in the environment, never in argv (ps shows every user's arguments)\n",
    )
    return 1
  }

  const kernel = await startKernel(config, { token })
  process.stdout.write(
    `${JSON.stringify({ port: kernel.port, pid: process.pid, version: VERSION })}\n`,
  )

  const stop = async (signal: string) => {
    info('stopping', { signal })
    // The Host waits 12 s after SIGTERM before SIGKILL; closing storage is the part that must fit.
    const timer = setTimeout(() => process.exit(1), 11_000)
    timer.unref()
    await kernel.stop()
    process.exit(0)
  }
  process.on('SIGTERM', () => void stop('SIGTERM'))
  process.on('SIGINT', () => void stop('SIGINT'))
  return new Promise<number>(() => {
    // The server keeps the process alive; nothing else has to.
  })
}

try {
  process.exitCode = await main(process.argv.slice(2))
} catch (cause) {
  error('the kernel could not start', {
    error: cause instanceof Error ? cause.message : String(cause),
    stack: cause instanceof Error ? cause.stack : undefined,
  })
  // A taken data directory is a refusal, not a crash: the Host may retry it, and must not file it as a bug.
  process.exitCode = cause instanceof LockError ? 2 : 1
}
