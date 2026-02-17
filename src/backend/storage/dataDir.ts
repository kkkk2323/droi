import { homedir } from 'os'
import { resolve, join } from 'path'

export function resolveServerDataDir(): string {
  const override = process.env['DROID_APP_DATA_DIR']
  if (override && override.trim()) return resolve(override.trim())
  return resolve(join(homedir(), '.droid-app'))
}

