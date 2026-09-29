/**
 * The Daemon's command line. `--settings` passes the Runtime Overlay; a Daemon
 * started without it has no Memory at all (ADR 0010).
 */
export function daemonArgs(options: {
  port: number
  liveness: string[]
  runtimeOverlay: string | null
}): string[] {
  return [
    'daemon',
    '--host',
    '127.0.0.1',
    '--port',
    String(options.port),
    ...options.liveness,
    ...(options.runtimeOverlay ? ['--settings', options.runtimeOverlay] : []),
  ]
}
