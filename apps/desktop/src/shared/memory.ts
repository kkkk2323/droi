// Contract between the Desktop Shell and the Local Client for Memory (ADR
// 0010). Only the Local Client has it; Memory's entries never cross here, only
// the size of each Memory and the Shell's own work on them.

/** One Project Memory, or the Global Memory when `workspace` is null. */
export interface MemoryRow {
  workspace: string | null
  entries: number
  chars: number
  /** Past this, Settings suggests consolidating. */
  softLimit: number
  overSoftLimit: boolean
  /** ISO timestamp; null before the first consolidation. */
  lastConsolidated: string | null
  /** A consolidation of this Memory is running. */
  consolidating: boolean
  /** Searches of this Memory since calls were first logged. */
  searches: number
  /** Searches that returned nothing. */
  emptySearches: number
  /** Entries no search has returned, corrections left out. */
  neverFound: number
}

export interface MemoryOverview {
  rows: MemoryRow[]
  /** ISO timestamp of the first logged Memory Server call; null before any. */
  loggedSince: string | null
}

export interface ConsolidationResult {
  /** Category slices the model's answer was applied to. */
  applied: number
  /** Slices whose answer was rejected or failed; their entries are unchanged. */
  rejected: number
}

export interface MemoryBridge {
  overview(): Promise<MemoryOverview>
  /** Consolidates one Memory, category by category; resolves when it is done. */
  consolidate(workspace: string | null): Promise<ConsolidationResult>
  openFolder(): Promise<void>
  resetPrompts(): Promise<void>
}

export const MEMORY_IPC = {
  overview: 'droi:memory:overview',
  consolidate: 'droi:memory:consolidate',
  openFolder: 'droi:memory:open-folder',
  resetPrompts: 'droi:memory:reset-prompts',
} as const
