import { resolve } from 'node:path'

export const VALIDATION_DROID_PATH_ENV = 'DROI_VALIDATION_DROID_PATH'
export const MISSION_VALIDATION_HARNESS_ENV = 'DROI_MISSION_VALIDATION_HARNESS'
export const DAEMON_FAILURE_MISSION_VALIDATION_HARNESS = 'daemon-failure'
export const KILL_WORKER_MISSION_VALIDATION_HARNESS = 'kill-worker'

const DAEMON_FAILURE_DROID_RELATIVE_PATH = 'scripts/mission-daemon-failure-droid.cjs'
const KILL_WORKER_DROID_RELATIVE_PATH = 'scripts/mission-kill-worker-droid.cjs'

export function getValidationDroidPathOverride(
  env: NodeJS.ProcessEnv | Record<string, string | undefined> = process.env,
): string | undefined {
  const override = String(env[VALIDATION_DROID_PATH_ENV] || '').trim()
  return override || undefined
}

export function resolveMissionValidationHarnessDroidPath(params: {
  env?: NodeJS.ProcessEnv | Record<string, string | undefined>
  repoRoot: string
}): string | undefined {
  const env = params.env || process.env
  const explicit = getValidationDroidPathOverride(env)
  if (explicit) return explicit

  const harness = String(env[MISSION_VALIDATION_HARNESS_ENV] || '')
    .trim()
    .toLowerCase()
  if (harness === DAEMON_FAILURE_MISSION_VALIDATION_HARNESS) {
    return resolve(params.repoRoot, DAEMON_FAILURE_DROID_RELATIVE_PATH)
  }
  if (harness === KILL_WORKER_MISSION_VALIDATION_HARNESS) {
    return resolve(params.repoRoot, KILL_WORKER_DROID_RELATIVE_PATH)
  }
  return undefined
}

export function buildMissionValidationHarnessEnv(params: {
  env?: NodeJS.ProcessEnv | Record<string, string | undefined>
  repoRoot: string
}): Record<string, string | undefined> {
  const env = { ...(params.env || process.env) }
  const droidPath = resolveMissionValidationHarnessDroidPath({ env, repoRoot: params.repoRoot })
  if (droidPath) env[VALIDATION_DROID_PATH_ENV] = droidPath
  return env
}
