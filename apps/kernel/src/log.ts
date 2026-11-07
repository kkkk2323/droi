/** Where the kernel writes what it does. stdout carries only the handshake line. */
export type Level = 'debug' | 'info' | 'warn' | 'error'

const order: Record<Level, number> = { debug: 0, info: 1, warn: 2, error: 3 }
let threshold = order[process.env.DROI_KERNEL_LOG === 'debug' ? 'debug' : 'info']!

export function setLevel(level: Level): void {
  threshold = order[level]!
}

/** One JSON line on stderr: the Host reads stderr as its log, never as protocol. */
export function log(level: Level, message: string, fields?: Record<string, unknown>): void {
  if (order[level]! < threshold) return
  const line = JSON.stringify({ t: Date.now(), level, message, ...fields })
  process.stderr.write(line + '\n')
}

export function debug(message: string, fields?: Record<string, unknown>): void {
  log('debug', message, fields)
}
export function info(message: string, fields?: Record<string, unknown>): void {
  log('info', message, fields)
}
export function warn(message: string, fields?: Record<string, unknown>): void {
  log('warn', message, fields)
}
export function error(message: string, fields?: Record<string, unknown>): void {
  log('error', message, fields)
}
