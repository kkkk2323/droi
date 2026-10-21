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
  /** True when this computer's Host can start oh-my-pi (OMP) engine sessions. */
  ompAvailable?: boolean
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

/**
 * Where the Daemon keeps a Session's transcript on the computer, for pasting
 * into another Session. GET with the token and the Session id in the query;
 * answers 200 with a SessionFileFound, or 404 when there is no such file.
 */
export const GATEWAY_SESSION_FILE_PATH = '/session-file'
export const GATEWAY_SESSION_ID_QUERY = 'sessionId'

export interface SessionFileFound {
  path: string
}

export function gatewaySessionFileUrl(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): string {
  const url = new URL(GATEWAY_SESSION_FILE_PATH, gatewayHttpUrl)
  url.searchParams.set(GATEWAY_TOKEN_QUERY, pairingToken)
  url.searchParams.set(GATEWAY_SESSION_ID_QUERY, sessionId)
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

/** Live OMP sessions on the computer (Host-managed omp --mode rpc-ui). */
export const GATEWAY_OMP_SESSIONS_PATH = '/omp/sessions'
export const GATEWAY_OMP_SESSION_PATH = '/omp/session'
export const GATEWAY_OMP_PROMPT_PATH = '/omp/session/prompt'
export const GATEWAY_OMP_ABORT_PATH = '/omp/session/abort'
export const GATEWAY_OMP_MODELS_PATH = '/omp/session/models'
export const GATEWAY_OMP_MODEL_PATH = '/omp/session/model'
export const GATEWAY_OMP_EVENTS_PATH = '/omp/session/events'

export interface OmpSession {
  id: string
  title: string
  cwd: string
  model?: string
  working: boolean
  engine: 'omp' | string
  updatedAt?: number
}

export interface OmpMessage {
  id: string
  role: string
  text?: string
  thinking?: string
  streaming?: boolean
  toolName?: string
  toolId?: string
  toolArgs?: string
  toolOut?: string
  isError?: boolean
  timestamp?: number
}

export interface OmpModel {
  id: string
  name?: string
  provider?: string
}

function ompUrl(
  gatewayHttpUrl: string,
  pairingToken: string,
  endpoint: string,
  sessionId?: string,
): string {
  const url = new URL(endpoint, gatewayHttpUrl)
  url.searchParams.set(GATEWAY_TOKEN_QUERY, pairingToken)
  if (sessionId !== undefined) url.searchParams.set(GATEWAY_SESSION_ID_QUERY, sessionId)
  return url.toString()
}

export function gatewayOmpSessionsUrl(gatewayHttpUrl: string, pairingToken: string): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_SESSIONS_PATH)
}

export function gatewayOmpSessionUrl(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_SESSION_PATH, sessionId)
}

export function gatewayOmpPromptUrl(gatewayHttpUrl: string, pairingToken: string): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_PROMPT_PATH)
}

export function gatewayOmpAbortUrl(gatewayHttpUrl: string, pairingToken: string): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_ABORT_PATH)
}

export function gatewayOmpModelsUrl(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_MODELS_PATH, sessionId)
}

export function gatewayOmpModelUrl(gatewayHttpUrl: string, pairingToken: string): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_MODEL_PATH)
}

export function gatewayOmpEventsUrl(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): string {
  return ompUrl(gatewayHttpUrl, pairingToken, GATEWAY_OMP_EVENTS_PATH, sessionId)
}
