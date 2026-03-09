import { spawn } from 'node:child_process'
import process from 'node:process'
import { cleanupListeningPorts, MISSION_E2E_PORTS } from '../src/backend/utils/portCleanup.ts'

const env = {
  ...process.env,
  DROID_APP_DATA_DIR: process.env.DROID_APP_DATA_DIR || '/tmp/droi-mission-e2e',
  DROID_APP_API_PORT: process.env.DROID_APP_API_PORT || '3002',
  ELECTRON_REMOTE_DEBUGGING_PORT: process.env.ELECTRON_REMOTE_DEBUGGING_PORT || '9222',
}

async function main() {
  await cleanupListeningPorts(MISSION_E2E_PORTS)

  const child = spawn('pnpm', ['exec', 'electron-vite', 'dev'], {
    stdio: 'inherit',
    env,
  })

  child.on('exit', (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal)
      return
    }
    process.exit(code ?? 0)
  })
}

main().catch((error) => {
  console.error('Mission dev:test launcher failed', error)
  process.exit(1)
})
