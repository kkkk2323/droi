// Contract between the Desktop Shell and the Local Client for Shell settings.
// Only the Local Client can reach these; a Remote Client has no preload.

export interface FactoryAccount {
  userId: string
  orgId: string | null
  email: string | null
}

export interface LoginPending {
  userCode: string
  verificationUri: string
  verificationUriComplete: string
  /** Unix ms when the code stops working. */
  expiresAt: number
}

/** "Sign in with Factory": the same device flow as `droid login`. */
export type LoginState =
  | { status: 'signed-out'; error: string | null }
  | { status: 'pending'; pending: LoginPending }
  | { status: 'signed-in'; account: FactoryAccount }

export interface ShellSettingsSnapshot {
  login: LoginState
  /**
   * Who the Daemon itself runs as: the `droid` CLI's login on this computer
   * (from ~/.factory/host.json). Sessions belong to this identity, so the
   * Droi login must match it.
   */
  daemonIdentity: { userId: string; orgId: string | null } | null
  remoteAccess: boolean
  droidPath: string | null
  /** FACTORY_API_BASE_URL handed to the Daemon; null keeps its default. */
  factoryApiBaseUrl: string | null
  /** True when FACTORY_API_BASE_URL is set in the Shell's environment. */
  factoryApiBaseUrlFromEnvironment: string | null
  /** Whether a Factory API key is stored or supplied by the environment. Never the key. */
  hasApiKey: boolean
  /** True when the Gateway has something to authenticate with (login or key). */
  hasCredential: boolean
  /** True when the key comes from FACTORY_API_KEY and cannot be edited here. */
  apiKeyFromEnvironment: boolean
  /** Whether the `droid` executable was found (override, PATH or ~/.local/bin). */
  droidFound: string | null
  version: string
}

export interface PairingInfo {
  /** Full link a phone opens: http://<lan-ip>:<port>/#pair=<token> */
  link: string | null
  /** LAN addresses the Gateway listens on; empty while Remote Access is off. */
  lanAddresses: string[]
  port: number
}

export interface ShellSettingsPatch {
  remoteAccess?: boolean
  droidPath?: string | null
  factoryApiBaseUrl?: string | null
}

export interface ShellSettingsBridge {
  get(): Promise<ShellSettingsSnapshot>
  update(patch: ShellSettingsPatch): Promise<ShellSettingsSnapshot>
  setApiKey(apiKey: string | null): Promise<ShellSettingsSnapshot>
  /** Starts the device flow and opens the browser; the snapshot turns `pending`. */
  signIn(): Promise<ShellSettingsSnapshot>
  cancelSignIn(): Promise<ShellSettingsSnapshot>
  signOut(): Promise<ShellSettingsSnapshot>
  resetPairingToken(): Promise<PairingInfo>
  getPairing(): Promise<PairingInfo>
  onChange(listener: () => void): () => void
}

export const SHELL_IPC = {
  get: 'droi:settings:get',
  update: 'droi:settings:update',
  setApiKey: 'droi:settings:set-api-key',
  signIn: 'droi:settings:sign-in',
  cancelSignIn: 'droi:settings:cancel-sign-in',
  signOut: 'droi:settings:sign-out',
  resetPairingToken: 'droi:settings:reset-pairing-token',
  getPairing: 'droi:settings:get-pairing',
  changed: 'droi:settings:changed',
} as const
