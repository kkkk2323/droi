// Desktop Shell settings: a small JSON file created mode 0600 in the user's
// data directory. The Factory login, API key and Pairing Token live in it in
// clear, like the droid CLI keeps its own credentials under ~/.factory; the
// user's home directory is the trust boundary. The key is never handed to a
// Client, only the Gateway reads it.
import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { randomBytes, randomUUID } from 'node:crypto'

export interface ShellSettings {
  remoteAccess: boolean
  pairingToken: string
  /**
   * Stable id of this computer, made once and never reset; the Gateway's
   * /meta reports it so a phone recognises a computer it paired before.
   */
  computerId: string
  /** Explicit path to the `droid` executable; null means auto-detect. */
  droidPath: string | null
  /**
   * Base URL the Daemon uses for Factory's API (FACTORY_API_BASE_URL), for a
   * local proxy such as droid-proxy; null keeps the Daemon's default.
   */
  factoryApiBaseUrl: string | null
  /**
   * Text the Gateway appends to Droid's own system prompt in every Session a
   * Client starts; null leaves the prompt as Droid ships it.
   */
  appendSystemPrompt: string | null
  /**
   * Host name the pairing link uses instead of the first LAN address, for a
   * name that also reaches this computer from elsewhere (Tailscale, Surge Ponte).
   */
  pairingHost: string | null
  /** Where Scratch Workspaces are made (ADR 0008); null for ~/.droi/chats. */
  scratchFolder: string | null
}

export interface ShellSettingsStoreOptions {
  file: string
  env?: NodeJS.ProcessEnv
  /**
   * Decrypts values written by versions that kept secrets under Electron
   * safeStorage (`*Encrypted` fields); they are moved to clear on first load.
   * Failures are swallowed: the value is then simply gone.
   */
  decryptLegacy?: (stored: string) => string
}

export interface ShellSettingsStore {
  readonly settings: Readonly<ShellSettings>
  update(patch: Partial<Omit<ShellSettings, 'pairingToken' | 'computerId'>>): void
  resetPairingToken(): string
  getApiKey(): string | null
  setApiKey(apiKey: string | null): void
  /** Factory login tokens (see factory-auth.ts). */
  getLogin(): string | null
  setLogin(serialized: string | null): void
}

interface StoredFile {
  remoteAccess: boolean
  droidPath: string | null
  factoryApiBaseUrl: string | null
  appendSystemPrompt: string | null
  pairingHost: string | null
  scratchFolder: string | null
  pairingToken: string
  computerId: string
  apiKey: string | null
  login: string | null
}

/**
 * The host name in what the user typed for the pairing address: "laptop.myhome",
 * "laptop.myhome:41417" or a full URL all give "laptop.myhome"; the port is
 * always the Gateway's own. Null for empty or unusable input.
 */
export function pairingHostOf(text: string | null | undefined): string | null {
  const trimmed = text?.trim()
  if (!trimmed) return null
  try {
    const { hostname } = new URL(
      /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`,
    )
    return /^(\[[0-9a-f:.]+\]|[a-z0-9.-]+)$/i.test(hostname) ? hostname : null
  } catch {
    return null
  }
}

/** What the user typed for the Scratch folder as an absolute path; ~ is the home directory. */
export function scratchFolderOf(text: string | null | undefined, home: string): string | null {
  const trimmed = text?.trim()
  if (!trimmed) return null
  const expanded =
    trimmed === '~' ? home : trimmed.startsWith('~/') ? join(home, trimmed.slice(2)) : trimmed
  return isAbsolute(expanded) ? resolve(expanded) : null
}

export function generatePairingToken(): string {
  return randomBytes(24).toString('base64url')
}

export function createShellSettingsStore(options: ShellSettingsStoreOptions): ShellSettingsStore {
  const env = options.env ?? process.env
  const loaded = load(options.file, options.decryptLegacy)

  const current: StoredFile = {
    remoteAccess: loaded?.remoteAccess ?? false,
    droidPath: loaded?.droidPath ?? null,
    factoryApiBaseUrl: loaded?.factoryApiBaseUrl ?? null,
    appendSystemPrompt: loaded?.appendSystemPrompt ?? null,
    pairingHost: loaded?.pairingHost ?? null,
    scratchFolder: loaded?.scratchFolder ?? null,
    pairingToken: loaded?.pairingToken || generatePairingToken(),
    computerId: loaded?.computerId || randomUUID(),
    apiKey: loaded?.apiKey ?? null,
    login: loaded?.login ?? null,
  }

  const save = () => {
    mkdirSync(dirname(options.file), { recursive: true })
    const tmp = `${options.file}.tmp`
    writeFileSync(tmp, JSON.stringify(current, null, 2), { mode: 0o600 })
    renameSync(tmp, options.file)
  }
  // Write straight away when the file is new, corrupt, in the old encrypted
  // layout, or from before the computer id (which must not change on the next load).
  if (!loaded || loaded.migrated || !loaded.computerId) save()

  return {
    get settings() {
      return {
        remoteAccess: current.remoteAccess,
        droidPath: current.droidPath,
        factoryApiBaseUrl: current.factoryApiBaseUrl,
        appendSystemPrompt: current.appendSystemPrompt,
        pairingHost: current.pairingHost,
        scratchFolder: current.scratchFolder,
        pairingToken: current.pairingToken,
        computerId: current.computerId,
      }
    },
    update(patch) {
      if (patch.remoteAccess !== undefined) current.remoteAccess = patch.remoteAccess
      if (patch.droidPath !== undefined) current.droidPath = patch.droidPath
      if (patch.factoryApiBaseUrl !== undefined) current.factoryApiBaseUrl = patch.factoryApiBaseUrl
      if (patch.appendSystemPrompt !== undefined) {
        current.appendSystemPrompt = patch.appendSystemPrompt
      }
      if (patch.pairingHost !== undefined) current.pairingHost = patch.pairingHost
      if (patch.scratchFolder !== undefined) current.scratchFolder = patch.scratchFolder
      save()
    },
    resetPairingToken() {
      current.pairingToken = generatePairingToken()
      save()
      return current.pairingToken
    },
    getApiKey() {
      return env['FACTORY_API_KEY'] || current.apiKey
    },
    setApiKey(apiKey) {
      current.apiKey = apiKey || null
      save()
    },
    getLogin() {
      return current.login
    },
    setLogin(serialized) {
      current.login = serialized || null
      save()
    },
  }
}

function load(
  file: string,
  decryptLegacy: ((stored: string) => string) | undefined,
): (Partial<StoredFile> & { migrated: boolean }) | null {
  let text: string
  try {
    text = readFileSync(file, 'utf8')
  } catch {
    return null
  }
  let parsed: Record<string, unknown>
  try {
    const value: unknown = JSON.parse(text)
    if (typeof value !== 'object' || value === null) return null
    parsed = value as Record<string, unknown>
  } catch {
    return null
  }
  const str = (key: string): string | null =>
    typeof parsed[key] === 'string' ? String(parsed[key]) : null
  const legacy = (key: string): string | null => {
    const stored = str(key)
    if (!stored || !decryptLegacy) return null
    try {
      return decryptLegacy(stored) || null
    } catch {
      return null
    }
  }
  const migrated =
    'pairingTokenEncrypted' in parsed || 'apiKeyEncrypted' in parsed || 'loginEncrypted' in parsed
  return {
    remoteAccess: parsed['remoteAccess'] === true,
    droidPath: str('droidPath'),
    factoryApiBaseUrl: str('factoryApiBaseUrl'),
    appendSystemPrompt: str('appendSystemPrompt'),
    pairingHost: str('pairingHost'),
    scratchFolder: str('scratchFolder'),
    pairingToken: str('pairingToken') ?? legacy('pairingTokenEncrypted') ?? undefined,
    computerId: str('computerId') ?? undefined,
    apiKey: str('apiKey') ?? legacy('apiKeyEncrypted'),
    login: str('login') ?? legacy('loginEncrypted'),
    migrated,
  }
}
