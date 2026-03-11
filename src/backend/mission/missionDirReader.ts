import { readFile, readdir, stat } from 'fs/promises'
import { homedir } from 'os'
import { join, resolve } from 'path'
import type { MissionDiskObject, MissionDirSnapshot } from './missionTypes.ts'
import {
  MISSION_FEATURES_FILE,
  MISSION_HANDOFFS_DIR,
  MISSION_PROGRESS_FILE,
  MISSION_STATE_FILE,
  MISSION_VALIDATION_STATE_FILE,
  MISSION_WORKING_DIRECTORY_FILE,
} from './missionTypes.ts'

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

async function pathExists(path: string): Promise<boolean> {
  try {
    return (await stat(path)).isDirectory()
  } catch {
    return false
  }
}

async function readJsonFile(path: string): Promise<unknown | null> {
  try {
    return JSON.parse(await readFile(path, 'utf8'))
  } catch {
    return null
  }
}

function normalizeFeatures(payload: unknown): MissionDiskObject[] | null {
  if (Array.isArray(payload)) {
    return payload.filter(isObject) as MissionDiskObject[]
  }
  if (isObject(payload) && Array.isArray(payload.features)) {
    return payload.features.filter(isObject) as MissionDiskObject[]
  }
  return null
}

function normalizeObject(payload: unknown): MissionDiskObject | null {
  return isObject(payload) ? (payload as MissionDiskObject) : null
}

function parseJsonLines(raw: string): MissionDiskObject[] {
  const lines = raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
  const entries: MissionDiskObject[] = []
  for (const line of lines) {
    try {
      const parsed = JSON.parse(line)
      if (isObject(parsed)) entries.push(parsed as MissionDiskObject)
    } catch {
      // Ignore malformed lines; the next poll/watch cycle can recover if the file is mid-write.
    }
  }
  return entries
}

export function resolveMissionDirPath(params: {
  sessionId: string
  missionDir?: string | null
  missionBaseSessionId?: string | null
}): string {
  const explicit = String(params.missionDir || '').trim()
  if (explicit) {
    if (explicit === '~') return homedir()
    if (explicit.startsWith('~/')) return join(homedir(), explicit.slice(2))
    return resolve(explicit)
  }

  const missionBaseSessionId = String(params.missionBaseSessionId || '').trim()
  const sessionId = missionBaseSessionId || String(params.sessionId || '').trim()
  return join(homedir(), '.factory', 'missions', sessionId)
}

export async function readMissionDirSnapshot(missionDir: string): Promise<MissionDirSnapshot> {
  const resolvedMissionDir = resolveMissionDirPath({ sessionId: '', missionDir })
  if (!(await pathExists(resolvedMissionDir))) {
    return {
      missionDir: resolvedMissionDir,
      exists: false,
      workingDirectory: undefined,
      state: null,
      features: null,
      progressEntries: [],
      handoffs: [],
      validationState: null,
    }
  }

  const [stateRaw, featuresRaw, validationStateRaw] = await Promise.all([
    readJsonFile(join(resolvedMissionDir, MISSION_STATE_FILE)),
    readJsonFile(join(resolvedMissionDir, MISSION_FEATURES_FILE)),
    readJsonFile(join(resolvedMissionDir, MISSION_VALIDATION_STATE_FILE)),
  ])

  let workingDirectory: string | undefined
  try {
    const raw = await readFile(join(resolvedMissionDir, MISSION_WORKING_DIRECTORY_FILE), 'utf8')
    const trimmed = raw.trim()
    workingDirectory = trimmed || undefined
  } catch {
    workingDirectory = undefined
  }

  let progressEntries: MissionDiskObject[] = []
  try {
    const raw = await readFile(join(resolvedMissionDir, MISSION_PROGRESS_FILE), 'utf8')
    progressEntries = parseJsonLines(raw)
  } catch {
    progressEntries = []
  }

  const handoffsDir = join(resolvedMissionDir, MISSION_HANDOFFS_DIR)
  const handoffs: MissionDirSnapshot['handoffs'] = []
  try {
    const files = (await readdir(handoffsDir))
      .filter((fileName) => fileName.endsWith('.json'))
      .sort()
    for (const fileName of files) {
      const parsed = await readJsonFile(join(handoffsDir, fileName))
      if (!isObject(parsed)) continue
      handoffs.push({ fileName, payload: parsed as MissionDiskObject })
    }
  } catch {
    // Late handoffs directory is expected.
  }

  return {
    missionDir: resolvedMissionDir,
    exists: true,
    workingDirectory,
    state: normalizeObject(stateRaw),
    features: normalizeFeatures(featuresRaw),
    progressEntries,
    handoffs,
    validationState: normalizeObject(validationStateRaw),
  }
}
