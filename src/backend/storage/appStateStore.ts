import { join } from 'path'
import { readFile } from 'fs/promises'
import { randomUUID } from 'crypto'
import type { ApiKeyEntry, PersistedAppState, PersistedAppStateV2, ProjectSettings } from '../../shared/protocol'
import { atomicWriteFile } from './fsUtils.ts'

function normalizeProjects(projects: unknown): Array<{ dir: string; name: string }> | undefined {
  if (!Array.isArray(projects)) return undefined
  const out: Array<{ dir: string; name: string }> = []
  for (const p of projects) {
    const dir = (p as any)?.dir
    const name = (p as any)?.name
    if (typeof dir === 'string' && dir && typeof name === 'string' && name) out.push({ dir, name })
  }
  return out.length ? out : undefined
}

function normalizeProjectSettings(raw: unknown): Record<string, ProjectSettings> | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const out: Record<string, ProjectSettings> = {}
  for (const [repoRoot, value] of Object.entries(raw as Record<string, unknown>)) {
    if (typeof repoRoot !== 'string' || !repoRoot.trim()) continue
    const baseBranch = typeof (value as any)?.baseBranch === 'string' ? String((value as any).baseBranch).trim() : ''
    const worktreePrefix = typeof (value as any)?.worktreePrefix === 'string' ? String((value as any).worktreePrefix).trim() : ''
    const setupScript = typeof (value as any)?.setupScript === 'string' ? String((value as any).setupScript).trim() : ''
    const settings: ProjectSettings = {}
    if (baseBranch) settings.baseBranch = baseBranch
    if (worktreePrefix) settings.worktreePrefix = worktreePrefix
    if (setupScript) settings.setupScript = setupScript
    if (settings.baseBranch || settings.worktreePrefix || settings.setupScript) out[repoRoot] = settings
  }
  return Object.keys(out).length ? out : undefined
}

function normalizeApiKeys(raw: unknown, legacyApiKey?: string): ApiKeyEntry[] | undefined {
  const out: ApiKeyEntry[] = []
  if (Array.isArray(raw)) {
    for (const entry of raw) {
      const key = typeof (entry as any)?.key === 'string' ? (entry as any).key.trim() : ''
      if (!key) continue
      const note = typeof (entry as any)?.note === 'string' ? (entry as any).note : undefined
      const addedAt = typeof (entry as any)?.addedAt === 'number' ? (entry as any).addedAt : Date.now()
      out.push({ key, note, addedAt })
    }
  }
  if (out.length === 0 && legacyApiKey) {
    out.push({ key: legacyApiKey, addedAt: Date.now() })
  }
  return out.length ? out : undefined
}

function migrateToV2(raw: any): PersistedAppStateV2 {
  if (raw && typeof raw === 'object' && raw.version === 2 && typeof raw.machineId === 'string' && raw.machineId) {
    const retentionDays = typeof raw.localDiagnosticsRetentionDays === 'number' && Number.isFinite(raw.localDiagnosticsRetentionDays)
      ? Math.max(1, Math.floor(raw.localDiagnosticsRetentionDays))
      : undefined
    const maxTotalMb = typeof raw.localDiagnosticsMaxTotalMb === 'number' && Number.isFinite(raw.localDiagnosticsMaxTotalMb)
      ? Math.max(1, Math.floor(raw.localDiagnosticsMaxTotalMb))
      : undefined
    const commitMessageModelId = typeof raw.commitMessageModelId === 'string' ? raw.commitMessageModelId.trim() : ''
    const apiKey = typeof raw.apiKey === 'string' ? raw.apiKey : undefined
    return {
      version: 2,
      machineId: raw.machineId,
      apiKey,
      apiKeys: normalizeApiKeys(raw.apiKeys, apiKey),
      projects: normalizeProjects(raw.projects),
      activeProjectDir: typeof raw.activeProjectDir === 'string' ? raw.activeProjectDir : undefined,
      traceChainEnabled: typeof raw.traceChainEnabled === 'boolean' ? raw.traceChainEnabled : undefined,
      showDebugTrace: typeof raw.showDebugTrace === 'boolean' ? raw.showDebugTrace : undefined,
      localDiagnosticsEnabled: typeof raw.localDiagnosticsEnabled === 'boolean' ? raw.localDiagnosticsEnabled : undefined,
      localDiagnosticsRetentionDays: retentionDays,
      localDiagnosticsMaxTotalMb: maxTotalMb,
      commitMessageModelId: commitMessageModelId || undefined,
      lanAccessEnabled: typeof raw.lanAccessEnabled === 'boolean' ? raw.lanAccessEnabled : undefined,
      projectSettings: normalizeProjectSettings(raw.projectSettings),
    }
  }

  if (raw && typeof raw === 'object') {
    const retentionDays = typeof raw.localDiagnosticsRetentionDays === 'number' && Number.isFinite(raw.localDiagnosticsRetentionDays)
      ? Math.max(1, Math.floor(raw.localDiagnosticsRetentionDays))
      : undefined
    const maxTotalMb = typeof raw.localDiagnosticsMaxTotalMb === 'number' && Number.isFinite(raw.localDiagnosticsMaxTotalMb)
      ? Math.max(1, Math.floor(raw.localDiagnosticsMaxTotalMb))
      : undefined
    const commitMessageModelId = typeof raw.commitMessageModelId === 'string' ? raw.commitMessageModelId.trim() : ''
    const apiKey = typeof raw.apiKey === 'string' ? raw.apiKey : undefined
    return {
      version: 2,
      machineId: randomUUID(),
      apiKey,
      apiKeys: normalizeApiKeys(raw.apiKeys, apiKey),
      projects: normalizeProjects(raw.projects),
      activeProjectDir: typeof raw.activeProjectDir === 'string' ? raw.activeProjectDir : undefined,
      traceChainEnabled: typeof raw.traceChainEnabled === 'boolean' ? raw.traceChainEnabled : undefined,
      showDebugTrace: typeof raw.showDebugTrace === 'boolean' ? raw.showDebugTrace : undefined,
      localDiagnosticsEnabled: typeof raw.localDiagnosticsEnabled === 'boolean' ? raw.localDiagnosticsEnabled : undefined,
      localDiagnosticsRetentionDays: retentionDays,
      localDiagnosticsMaxTotalMb: maxTotalMb,
      commitMessageModelId: commitMessageModelId || undefined,
      lanAccessEnabled: typeof raw.lanAccessEnabled === 'boolean' ? raw.lanAccessEnabled : undefined,
      projectSettings: normalizeProjectSettings(raw.projectSettings),
    }
  }

  return { version: 2, machineId: randomUUID() }
}

export interface AppStateStore {
  load: () => Promise<PersistedAppState>
  save: (state: PersistedAppState) => Promise<void>
  update: (patch: Partial<Omit<PersistedAppStateV2, 'version' | 'machineId'>>) => Promise<PersistedAppState>
  filePath: string
}

export function createAppStateStore(opts: { baseDir: string }): AppStateStore {
  const filePath = join(opts.baseDir, 'app-state.json')

  const load = async (): Promise<PersistedAppState> => {
    try {
      const raw = JSON.parse(await readFile(filePath, 'utf-8'))
      return migrateToV2(raw)
    } catch {
      const next = migrateToV2(null)
      await atomicWriteFile(filePath, JSON.stringify(next, null, 2))
      return next
    }
  }

  const save = async (state: PersistedAppState): Promise<void> => {
    const normalized: PersistedAppStateV2 = migrateToV2(state)
    await atomicWriteFile(filePath, JSON.stringify(normalized, null, 2))
  }

  const update = async (patch: Partial<Omit<PersistedAppStateV2, 'version' | 'machineId'>>): Promise<PersistedAppState> => {
    const cur = (await load()) as PersistedAppStateV2
    const next: PersistedAppStateV2 = migrateToV2({ ...cur, ...patch, version: 2, machineId: cur.machineId })
    await save(next)
    return next
  }

  return { load, save, update, filePath }
}
