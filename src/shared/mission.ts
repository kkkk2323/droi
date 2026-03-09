export const MISSION_STATE_FILE = 'state.json'
export const MISSION_FEATURES_FILE = 'features.json'
export const MISSION_PROGRESS_FILE = 'progress_log.jsonl'
export const MISSION_HANDOFFS_DIR = 'handoffs'
export const MISSION_VALIDATION_STATE_FILE = 'validation-state.json'

export type MissionDiskObject = Record<string, unknown>

export interface MissionDiskHandoff {
  fileName: string
  payload: MissionDiskObject
}

export interface MissionLoadSnapshot {
  state?: MissionDiskObject | null
  features?: MissionDiskObject[] | null
  progressLog?: MissionDiskObject[] | null
  progressEntries?: MissionDiskObject[] | null
  handoffs?: MissionDiskHandoff[] | Record<string, MissionDiskObject> | null
  validationState?: MissionDiskObject | null
}

export interface MissionDirSnapshot {
  missionDir: string
  exists: boolean
  state: MissionDiskObject | null
  features: MissionDiskObject[] | null
  progressEntries: MissionDiskObject[]
  handoffs: MissionDiskHandoff[]
  validationState: MissionDiskObject | null
}

export interface MissionDirRequest {
  sessionId: string
  missionDir?: string | null
  missionBaseSessionId?: string | null
}

export interface MissionDirReadResult {
  sessionId: string
  snapshot: MissionDirSnapshot
}

export type MissionDirChangeSource = 'initial' | 'fs-watch' | 'poll'

export interface MissionDirChangeEvent {
  sessionId: string
  missionDir: string
  changedPaths: string[]
  source: MissionDirChangeSource
  snapshot: MissionDirSnapshot
}
