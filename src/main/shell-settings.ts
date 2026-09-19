// Desktop Shell settings: a small JSON file. The Factory API key is stored
// encrypted (Electron safeStorage in production) and is never handed to a
// Client; only the Gateway reads it.
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

interface StoredFile extends ShellSettings {
  apiKeyEncrypted: string | null
}

export function generatePairingToken(): string {
  return randomBytes(24).toString('base64url')
}

export function createShellSettingsStore(options: ShellSettingsStoreOptions): ShellSettingsStore {
  const env = options.env ?? process.env
  let stored = load(options.file)

  const save = () => {
    mkdirSync(dirname(options.file), { recursive: true })
    const tmp = `${options.file}.tmp`
    writeFileSync(tmp, JSON.stringify(stored, null, 2), { mode: 0o600 })
    renameSync(tmp, options.file)
  }
  if (!stored) {
    stored = {
      remoteAccess: false,
      pairingToken: generatePairingToken(),
      droidPath: null,
      apiKeyEncrypted: null,
    }
    save()
  }
  const current = stored

  return {
    get settings() {
      const { apiKeyEncrypted: _omitted, ...settings } = current
      return settings
    },
    update(patch) {
      Object.assign(current, patch)
      save()
    },
    resetPairingToken() {
      current.pairingToken = generatePairingToken()
      save()
      return current.pairingToken
    },
    getApiKey() {
      const fromEnv = env['FACTORY_API_KEY']
      if (fromEnv) return fromEnv
      if (!current.apiKeyEncrypted) return null
      try {
        return options.cipher.decrypt(current.apiKeyEncrypted)
      } catch {
        return null
      }
    },
    setApiKey(apiKey) {
      current.apiKeyEncrypted = apiKey ? options.cipher.encrypt(apiKey) : null
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
    if (typeof parsed.pairingToken !== 'string') return null
    return {
      remoteAccess: parsed.remoteAccess === true,
      pairingToken: parsed.pairingToken,
      droidPath: typeof parsed.droidPath === 'string' ? parsed.droidPath : null,
      apiKeyEncrypted: typeof parsed.apiKeyEncrypted === 'string' ? parsed.apiKeyEncrypted : null,
    }
  } catch {
    return null
  }
}
