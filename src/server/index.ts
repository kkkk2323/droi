import { join, resolve } from 'path'
import { startApiServer } from './apiServer.ts'
import { resolveServerDataDir } from '../backend/storage/dataDir.ts'

function readBool(name: string, def: boolean): boolean {
  const raw = (process.env[name] || '').trim().toLowerCase()
  if (!raw) return def
  return raw === '1' || raw === 'true' || raw === 'yes' || raw === 'on'
}

function resolveWebRootDir(): string | null {
  const env = (process.env['DROID_WEB_ROOT_DIR'] || '').trim()
  if (env) return env
  const candidate = resolve(join(process.cwd(), 'out', 'renderer'))
  return candidate
}

async function main() {
  const enabled = readBool('DROID_WEB_ENABLED', true)
  const host = (process.env['DROID_APP_API_HOST'] || (enabled ? '0.0.0.0' : '127.0.0.1')).trim() || '127.0.0.1'
  const port = Number(process.env['DROID_APP_API_PORT'] || 3001)
  const baseDir = (process.env['DROID_APP_DATA_DIR'] || '').trim() || resolveServerDataDir()
  const webRootDir = enabled ? resolveWebRootDir() : null

  const started = await startApiServer({
    host,
    port,
    baseDir,
    webRootDir,
  })

  // eslint-disable-next-line no-console
  console.log(`Droid API server running at http://${started.host}:${started.port}`)
  // eslint-disable-next-line no-console
  console.log(`Data dir: ${started.baseDir}`)
  // eslint-disable-next-line no-console
  console.log(`Web UI: ${enabled ? (webRootDir || '(missing)') : 'disabled'}`)
}

void main()

