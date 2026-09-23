// Command-line switches the Desktop Shell passes to the preload via
// `additionalArguments`. Shared with the preload so the names cannot drift.
export const SHELL_ARG_GATEWAY_URL = '--droi-gateway-url'
export const SHELL_ARG_PAIRING_TOKEN = '--droi-pairing-token'

export function readShellArg(argv: readonly string[], name: string): string | null {
  const prefix = `${name}=`
  const match = argv.find((arg) => arg.startsWith(prefix))
  return match ? match.slice(prefix.length) : null
}
