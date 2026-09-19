// Desktop Shell settings: a small JSON file. The Factory API key and the
// Pairing Token are stored encrypted (Electron safeStorage in production); the
// key is never handed to a Client, only the Gateway reads it.
import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname } from 'node:path'
import { randomBytes } from 'node:crypto'

export interface ShellSettings {
  remoteAccess: boolean
  pairingToken: string
  /** Explicit path to the `droid` executable; null means auto-detect. */
  droidPath: string | null
}

export interface SettingsCipher {
  encrypt(plain: string): string
  decrypt(stored: string): string
}

export interface ShellSettingsStoreOptions {
  file: string
  cipher: SettingsCipher
  env?: NodeJS.ProcessEnv
}

export interface ShellSettingsStore {
  readonly settings: Readonly<ShellSettings>
  update(patch: Partial<Omit<ShellSettings, 'pairingToken'>>): void
  resetPairingToken(): string
  getApiKey(): string | null
  setApiKey(apiKey: string | null): void
}

interface StoredFile {
  remoteAccess: boolean
  droidPath: string | null
  pairingTokenEncrypted: string
  apiKeyEncrypted: string | null
}

export function generatePairingToken(): string {
  return randomBytes(24).toString('base64url')
}

export function createShellSettingsStore(options: ShellSettingsStoreOptions): ShellSettingsStore {
  const env = options.env ?? process.env
  const { cipher } = options
  const loaded = load(options.file)
  const decryptToken = (encrypted: string | undefined): string | null => {
    if (!encrypted) return null
    try {
      return cipher.decrypt(encrypted) || null
    } catch {
      return null
    }
  }

  let pairingToken = decryptToken(loaded?.pairingTokenEncrypted) ?? generatePairingToken()
  const current: StoredFile = {
    remoteAccess: loaded?.remoteAccess ?? false,
    droidPath: loaded?.droidPath ?? null,
    pairingTokenEncrypted: cipher.encrypt(pairingToken),
    apiKeyEncrypted: loaded?.apiKeyEncrypted ?? null,
  }

  const save = () => {
    mkdirSync(dirname(options.file), { recursive: true })
    const tmp = `${options.file}.tmp`
    writeFileSync(tmp, JSON.stringify(current, null, 2), { mode: 0o600 })
    renameSync(tmp, options.file)
  }
  if (!loaded || !decryptToken(loaded.pairingTokenEncrypted)) save()

  return {
    get settings() {
      return { remoteAccess: current.remoteAccess, droidPath: current.droidPath, pairingToken }
    },
    update(patch) {
      if (patch.remoteAccess !== undefined) current.remoteAccess = patch.remoteAccess
      if (patch.droidPath !== undefined) current.droidPath = patch.droidPath
      save()
    },
    resetPairingToken() {
      pairingToken = generatePairingToken()
      current.pairingTokenEncrypted = cipher.encrypt(pairingToken)
      save()
      return pairingToken
    },
    getApiKey() {
      const fromEnv = env['FACTORY_API_KEY']
      if (fromEnv) return fromEnv
      if (!current.apiKeyEncrypted) return null
      try {
        return cipher.decrypt(current.apiKeyEncrypted)
      } catch {
        return null
      }
    },
    setApiKey(apiKey) {
      current.apiKeyEncrypted = apiKey ? cipher.encrypt(apiKey) : null
      save()
    },
  }
}

function load(file: string): StoredFile | null {
  let text: string
  try {
    text = readFileSync(file, 'utf8')
  } catch {
    return null
  }
  try {
    const parsed = JSON.parse(text) as Partial<StoredFile>
    if (typeof parsed !== 'object' || parsed === null) return null
    return {
      remoteAccess: parsed.remoteAccess === true,
      droidPath: typeof parsed.droidPath === 'string' ? parsed.droidPath : null,
      pairingTokenEncrypted:
        typeof parsed.pairingTokenEncrypted === 'string' ? parsed.pairingTokenEncrypted : '',
      apiKeyEncrypted: typeof parsed.apiKeyEncrypted === 'string' ? parsed.apiKeyEncrypted : null,
    }
  } catch {
    return null
  }
}
