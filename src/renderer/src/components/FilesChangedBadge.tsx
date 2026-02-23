import React, { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown, ChevronRight, FileEdit, FilePlus, FileX, FileCode } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { GitStatusFile } from '@/types'
import { useGitStatusQuery } from '@/hooks/useGitStatus'
import { AnimatedNumber } from '@/components/AnimatedNumber'

const STATUS_ICON_MAP: Record<string, { icon: typeof FileEdit; color: string }> = {
  M: { icon: FileEdit, color: 'text-amber-500' },
  A: { icon: FilePlus, color: 'text-emerald-500' },
  D: { icon: FileX, color: 'text-red-500' },
  '??': { icon: FileCode, color: 'text-muted-foreground' },
}

interface FilesChangedBadgeProps {
  projectDir: string
  isRunning: boolean
}

export function FilesChangedBadge({ projectDir, isRunning }: FilesChangedBadgeProps) {
  const { data: gitFiles = [], refetch } = useGitStatusQuery(projectDir, !isRunning)
  const [expanded, setExpanded] = useState(false)
  const prevIsRunning = useRef(isRunning)

  useEffect(() => {
    if (prevIsRunning.current && !isRunning) {
      void refetch()
    }
    prevIsRunning.current = isRunning
  }, [isRunning, refetch])

  const totals = useMemo(() => {
    let additions = 0
    let deletions = 0
    for (const f of gitFiles) {
      additions += f.additions
      deletions += f.deletions
    }
    return { additions, deletions }
  }, [gitFiles])

  if (gitFiles.length === 0) return null

  return (
    <div className="relative">
      <button
        className="flex items-center gap-1.5 rounded-md border border-border bg-card/80 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="text-emerald-500">
          +<AnimatedNumber value={totals.additions} />
        </span>
        <span className="text-red-500">
          -<AnimatedNumber value={totals.deletions} />
        </span>
        <span className="text-muted-foreground/60">
          (<AnimatedNumber value={gitFiles.length} />)
        </span>
      </button>

      {expanded && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setExpanded(false)} />
          <div className="absolute right-0 top-full z-50 mt-1 w-72 rounded-lg border border-border bg-card shadow-lg">
            <div className="flex items-center justify-between border-b border-border px-3 py-2">
              <span className="text-xs font-medium text-foreground/80">Files Changed</span>
              <span className="text-xs text-muted-foreground">{gitFiles.length} files</span>
            </div>
            <div className="max-h-64 overflow-auto py-1">
              {gitFiles.map((file) => {
                const info = STATUS_ICON_MAP[file.status] || STATUS_ICON_MAP['M']!
                const Icon = info.icon
                return (
                  <div
                    key={file.path}
                    className="flex items-center gap-2 px-3 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors"
                  >
                    <Icon className={cn('size-3 shrink-0', info.color)} />
                    <span className="truncate font-mono flex-1">{file.path}</span>
                    <span className="shrink-0 tabular-nums">
                      {file.additions > 0 && (
                        <span className="text-emerald-500">+{file.additions}</span>
                      )}
                      {file.additions > 0 && file.deletions > 0 && (
                        <span className="text-muted-foreground/40"> </span>
                      )}
                      {file.deletions > 0 && (
                        <span className="text-red-500">-{file.deletions}</span>
                      )}
                      {file.additions === 0 && file.deletions === 0 && (
                        <span className="text-muted-foreground/50">--</span>
                      )}
                    </span>
                  </div>
                )
              })}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
