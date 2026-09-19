// Contract between the Desktop Shell and the Local Client for Shell settings.
// Only the Local Client can reach these; a Remote Client has no preload.

export interface ShellSettingsSnapshot {
  remoteAccess: boolean
  droidPath: string | null
  /** Whether a Factory API key is stored or supplied by the environment. Never the key. */
  hasApiKey: boolean
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

export interface ShellSettingsBridge {
  get(): Promise<ShellSettingsSnapshot>
  update(patch: {
    remoteAccess?: boolean
    droidPath?: string | null
  }): Promise<ShellSettingsSnapshot>
  setApiKey(apiKey: string | null): Promise<ShellSettingsSnapshot>
  resetPairingToken(): Promise<PairingInfo>
  getPairing(): Promise<PairingInfo>
  onChange(listener: () => void): () => void
}

export const SHELL_IPC = {
  get: 'droi:settings:get',
  update: 'droi:settings:update',
  setApiKey: 'droi:settings:set-api-key',
  resetPairingToken: 'droi:settings:reset-pairing-token',
  getPairing: 'droi:settings:get-pairing',
  changed: 'droi:settings:changed',
} as const
