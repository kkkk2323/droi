import React, { useMemo, useState, useRef, useEffect } from 'react'
import { Popover } from '@base-ui/react/popover'
import { GitBranch, FolderOpen, ChevronDown, Search, Plus, Check } from 'lucide-react'
import { useGitBranchesQuery, useGitWorktreeBranchesInUseQuery } from '@/hooks/useGitStatus'
import { SessionBootstrapCards } from '@/components/SessionBootstrapCards'
import {
  usePendingNewSession,
  useIsCreatingSession,
  useWorkspaceError,
  useActions,
} from '@/store'

function normalizeBranches(raw: string[]): string[] {
  return Array.from(new Set((raw || []).filter(Boolean)))
    .map((b) => b.trim())
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b))
}

function BranchPicker({
  branches,
  inUseBranches,
  loading,
  selected,
  isExistingBranch,
  onSelectExisting,
  onSelectNewWorktree,
}: {
  branches: string[]
  inUseBranches: string[]
  loading: boolean
  selected: string
  isExistingBranch: boolean
  onSelectExisting: (branch: string) => void
  onSelectNewWorktree: () => void
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setSearch('')
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  const filteredAvailable = useMemo(() => {
    if (!search.trim()) return branches
    const q = search.toLowerCase()
    return branches.filter((b) => b.toLowerCase().includes(q))
  }, [branches, search])

  const filteredInUse = useMemo(() => {
    if (!search.trim()) return inUseBranches
    const q = search.toLowerCase()
    return inUseBranches.filter((b) => b.toLowerCase().includes(q))
  }, [inUseBranches, search])

  const isNewWorktree = !isExistingBranch

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        render={
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded-md border border-border bg-background px-2.5 py-1 text-xs text-foreground hover:bg-accent transition-colors"
          />
        }
      >
        <GitBranch className="size-3 text-muted-foreground" />
        <span className="max-w-[180px] truncate font-mono">
          {isNewWorktree ? 'New worktree' : selected}
        </span>
        <ChevronDown className="size-3 text-muted-foreground" />
      </Popover.Trigger>

      <Popover.Portal>
        <Popover.Positioner side="bottom" align="start" sideOffset={4} className="isolate z-50">
          <Popover.Popup className="bg-popover text-popover-foreground ring-foreground/10 w-64 rounded-lg shadow-lg ring-1 overflow-hidden outline-none data-open:animate-in data-closed:animate-out data-closed:fade-out-0 data-open:fade-in-0 data-closed:zoom-out-95 data-open:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 duration-100">
            <div className="flex items-center gap-2 border-b border-border px-3 py-2">
              <Search className="size-3.5 text-muted-foreground shrink-0" />
              <input
                ref={inputRef}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search branches"
                className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground outline-none"
              />
            </div>
            <div className="max-h-52 overflow-y-auto py-1">
              {!search.trim() && (
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-sm hover:bg-accent transition-colors text-left"
                  onClick={() => {
                    onSelectNewWorktree()
                    setOpen(false)
                  }}
                >
                  <Plus className="size-3.5 text-muted-foreground shrink-0" />
                  <span className="flex-1 text-xs">New worktree</span>
                  {isNewWorktree && <Check className="size-3.5 text-emerald-600 shrink-0" />}
                </button>
              )}
              {!search.trim() && branches.length > 0 && (
                <div className="mx-2 my-1 h-px bg-border" />
              )}
              {loading && (
                <div className="px-3 py-2 text-xs text-muted-foreground">Loading...</div>
              )}
              {!loading && filteredAvailable.length === 0 && filteredInUse.length === 0 && (
                <div className="px-3 py-2 text-xs text-muted-foreground">No branches found</div>
              )}
              {filteredAvailable.map((b) => (
                <button
                  key={b}
                  type="button"
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-sm hover:bg-accent transition-colors text-left"
                  onClick={() => {
                    onSelectExisting(b)
                    setOpen(false)
                  }}
                >
                  <GitBranch className="size-3.5 text-muted-foreground shrink-0" />
                  <span className="flex-1 truncate font-mono text-xs">{b}</span>
                  {isExistingBranch && b === selected && <Check className="size-3.5 text-emerald-600 shrink-0" />}
                </button>
              ))}

              {!loading && filteredInUse.length > 0 && (
                <>
                  <div className="mx-2 my-1 h-px bg-border" />
                  <div className="px-3 py-1 text-[10px] uppercase tracking-wide text-muted-foreground/70">
                    In use by another worktree
                  </div>
                </>
              )}

              {filteredInUse.map((b) => (
                <button
                  key={`inuse:${b}`}
                  type="button"
                  disabled
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-sm text-left opacity-50 cursor-not-allowed"
                >
                  <GitBranch className="size-3.5 text-muted-foreground shrink-0" />
                  <span className="flex-1 truncate font-mono text-xs">{b}</span>
                  <span className="text-[10px] text-muted-foreground shrink-0">In use</span>
                  {isExistingBranch && b === selected && <Check className="size-3.5 text-emerald-600 shrink-0" />}
                </button>
              ))}
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

export function SessionConfigPage() {
  const pending = usePendingNewSession()
  const isCreatingSession = useIsCreatingSession()
  const workspaceError = useWorkspaceError()
  const { updatePendingNewSession } = useActions()

  const repoRoot = String(pending?.repoRoot || '').trim()

  const { data: rawBranches = [], isLoading: loadingBranches } = useGitBranchesQuery(repoRoot, Boolean(repoRoot))
  const branches = useMemo(() => normalizeBranches(rawBranches || []), [rawBranches])

  const { data: inUse = [], isLoading: loadingInUse } = useGitWorktreeBranchesInUseQuery(repoRoot, Boolean(repoRoot))
  const inUseSet = useMemo(() => new Set((inUse || []).map((x) => String((x as any)?.branch || '').trim()).filter(Boolean)), [inUse])
  const availableBranches = useMemo(() => branches.filter((b) => !inUseSet.has(b)), [branches, inUseSet])
  const inUseBranches = useMemo(() => branches.filter((b) => inUseSet.has(b)), [branches, inUseSet])

  if (!pending) return null

  const repoName = repoRoot.split('/').pop() || repoRoot
  const workFromLabel = pending.isExistingBranch ? 'Work from Branch:' : 'Work from:'

  return (
    <div className="flex flex-1 items-center justify-center px-6">
      <div className="w-full max-w-md space-y-5">
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-muted">
            <GitBranch className="size-7 text-muted-foreground" />
          </div>
          <div>
            <h2 className="text-base font-medium text-foreground">New Session</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Send your first message to start
            </p>
          </div>
          <div className="flex items-center gap-2">
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground/70">
              <FolderOpen className="size-3" />
              <span className="font-mono">{repoName}</span>
            </span>
            <span className="text-xs text-muted-foreground/70">{workFromLabel}</span>
            <BranchPicker
              branches={availableBranches}
              inUseBranches={inUseBranches}
              loading={loadingBranches || loadingInUse}
              selected={pending.branch}
              isExistingBranch={Boolean(pending.isExistingBranch)}
              onSelectExisting={(branch) => updatePendingNewSession({ branch, isExistingBranch: true })}
              onSelectNewWorktree={() => updatePendingNewSession({ branch: '', isExistingBranch: false })}
            />
          </div>
        </div>

        {isCreatingSession && (
          <SessionBootstrapCards workspacePrepStatus="running" />
        )}

        {workspaceError && (
          <div className="text-sm text-center text-red-500">{workspaceError}</div>
        )}
      </div>
    </div>
  )
}
