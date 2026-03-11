import { readFile } from 'fs/promises'
import { homedir } from 'os'
import { join } from 'path'

import type { CustomModelDef, MissionModelSettings } from '../../shared/protocol'
import { atomicWriteFile } from './fsUtils.ts'

type FactorySettingsRecord = Record<string, unknown>

export function resolveFactorySettingsPath(): string {
  return join(homedir(), '.factory', 'settings.json')
}

async function readFactorySettingsFile(filePath: string): Promise<FactorySettingsRecord> {
  try {
    const raw = JSON.parse(await readFile(filePath, 'utf-8'))
    return raw && typeof raw === 'object' ? (raw as FactorySettingsRecord) : {}
  } catch {
    return {}
  }
}

function normalizeString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

export function normalizeMissionModelSettings(value: unknown): MissionModelSettings {
  if (!value || typeof value !== 'object') return {}
  const raw = value as Record<string, unknown>
  return {
    orchestratorModel: normalizeString(raw.orchestratorModel),
    workerModel: normalizeString(raw.workerModel),
    validationWorkerModel: normalizeString(raw.validationWorkerModel),
  }
}

export function readCustomModelsFromFactorySettings(value: unknown): CustomModelDef[] {
  const models = Array.isArray((value as any)?.customModels) ? (value as any).customModels : []
  return models
    .filter((model: any) => typeof model?.id === 'string' && typeof model?.displayName === 'string')
    .map((model: any) => ({
      id: model.id,
      displayName: model.displayName,
      model: String(model.model || ''),
      provider: String(model.provider || 'custom'),
    }))
}

export async function readFactoryMissionModelSettings(): Promise<MissionModelSettings> {
  return readMissionModelSettingsFromPath(resolveFactorySettingsPath())
}

export async function writeFactoryMissionModelSettings(
  settings: MissionModelSettings,
): Promise<MissionModelSettings> {
  return writeMissionModelSettingsToPath(resolveFactorySettingsPath(), settings)
}

export async function readMissionModelSettingsFromPath(
  filePath: string,
): Promise<MissionModelSettings> {
  const raw = await readFactorySettingsFile(filePath)
  return normalizeMissionModelSettings(raw.missionModelSettings)
}

export async function writeMissionModelSettingsToPath(
  filePath: string,
  settings: MissionModelSettings,
): Promise<MissionModelSettings> {
  const raw = await readFactorySettingsFile(filePath)
  const normalized = normalizeMissionModelSettings(settings)
  await atomicWriteFile(
    filePath,
    JSON.stringify({ ...raw, missionModelSettings: normalized }, null, 2) + '\n',
  )
  return normalized
}

export async function readFactoryCustomModels(): Promise<CustomModelDef[]> {
  const raw = await readFactorySettingsFile(resolveFactorySettingsPath())
  return readCustomModelsFromFactorySettings(raw)
}
