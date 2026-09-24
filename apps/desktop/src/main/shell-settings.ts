// Desktop Shell settings: a small JSON file created mode 0600 in the user's
// data directory. The Factory login, API key and Pairing Token live in it in
// clear, like the droid CLI keeps its own credentials under ~/.factory; the
// user's home directory is the trust boundary. The key is never handed to a
// Client, only the Gateway reads it.
import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname } from 'node:path'
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
  pairingToken: string
  computerId: string
  apiKey: string | null
  login: string | null
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
    pairingToken: str('pairingToken') ?? legacy('pairingTokenEncrypted') ?? undefined,
    computerId: str('computerId') ?? undefined,
    apiKey: str('apiKey') ?? legacy('apiKeyEncrypted'),
    login: str('login') ?? legacy('loginEncrypted'),
    migrated,
  }
}
