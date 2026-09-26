// Contract between the Gateway (Desktop Shell) and the Client. Both sides import
// this file so the WebSocket path, the query parameter and the /meta shape
// cannot drift apart.

/**
 * WebSocket path the Client connects to; frames are forwarded to the Daemon.
 * A plain GET on the same path answers 204 or 401 so a Client can tell a bad
 * Pairing Token apart from an unreachable Daemon, which browsers hide behind
 * one opaque WebSocket error.
 */
export const GATEWAY_DAEMON_PATH = '/daemon'

/** Query parameter carrying the Pairing Token on the WebSocket upgrade. */
export const GATEWAY_TOKEN_QUERY = 'token'

/** Read-only endpoint describing the Desktop Shell. Never contains secrets. */
export const GATEWAY_META_PATH = '/meta'

export interface GatewayMeta {
  app: 'Droi'
  version: string
  remoteAccess: boolean
  /** The computer's display name, as the user named it in the system settings. */
  name: string
  /** Stable id of the computer; survives restarts, Pairing Token resets and address changes. */
  computerId: string
}

/**
 * Value the Client puts in `daemon.authenticate.params.apiKey`. The Gateway
 * replaces it with the real Factory API key, so its content never matters; a
 * fixed marker keeps the frame recognisable in logs and tests.
 */
export const GATEWAY_API_KEY_PLACEHOLDER = 'droi-gateway'

/**
 * Scratch Workspaces (ADR 0008): the Gateway answers these itself, POST with
 * the token in the query. Creating answers 201 with a ScratchWorkspaceCreated;
 * trash and restore take the folder in the `path` query parameter and answer
 * 204. No body and no custom header, so a browser sends them without a preflight.
 */
export const GATEWAY_SCRATCH_PATH = '/scratch-workspaces'
export const GATEWAY_SCRATCH_TRASH_PATH = '/scratch-workspaces/trash'
export const GATEWAY_SCRATCH_RESTORE_PATH = '/scratch-workspaces/restore'
export const GATEWAY_SCRATCH_PATH_QUERY = 'path'

export interface ScratchWorkspaceCreated {
  path: string
}

export function gatewayScratchUrl(
  gatewayHttpUrl: string,
  pairingToken: string,
  endpoint: string,
  path?: string,
): string {
  const url = new URL(endpoint, gatewayHttpUrl)
  url.searchParams.set(GATEWAY_TOKEN_QUERY, pairingToken)
  if (path !== undefined) url.searchParams.set(GATEWAY_SCRATCH_PATH_QUERY, path)
  return url.toString()
}

export function gatewayDaemonUrl(gatewayHttpUrl: string, pairingToken: string): string {
  const url = new URL(GATEWAY_DAEMON_PATH, gatewayHttpUrl)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.searchParams.set(GATEWAY_TOKEN_QUERY, pairingToken)
  return url.toString()
}

export function gatewayPairingCheckUrl(gatewayHttpUrl: string, pairingToken: string): string {
  const url = new URL(GATEWAY_DAEMON_PATH, gatewayHttpUrl)
  url.searchParams.set(GATEWAY_TOKEN_QUERY, pairingToken)
  return url.toString()
}
