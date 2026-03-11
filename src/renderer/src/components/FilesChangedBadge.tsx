import React, { useEffect, useMemo, useRef } from 'react'
import { FileEdit, FilePlus, FileX, FileCode } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useGitStatusQuery } from '@/hooks/useGitStatus'
import { AnimatedNumber } from '@/components/AnimatedNumber'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from '@/components/ui/dropdown-menu'

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
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<button type="button" />}
        className="flex items-center gap-1.5 rounded-md border border-border bg-card/80 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent"
      >
        <span className="text-emerald-500">
          +<AnimatedNumber value={totals.additions} />
        </span>
        <span className="text-red-500">
          -<AnimatedNumber value={totals.deletions} />
        </span>
        <span className="text-muted-foreground">
          (<AnimatedNumber value={gitFiles.length} />)
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={6} className="w-72 p-0">
        <div className="flex items-center justify-between border-b border-border px-3 py-2">
          <span className="text-xs font-medium text-foreground">Files Changed</span>
          <span className="text-xs text-muted-foreground">{gitFiles.length} files</span>
        </div>
        <div className="max-h-64 overflow-auto py-1">
          {gitFiles.map((file) => {
            const info = STATUS_ICON_MAP[file.status] || STATUS_ICON_MAP['M']!
            const Icon = info.icon
            return (
              <div
                key={file.path}
                className="flex items-center gap-2 px-3 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                <Icon className={cn('size-3 shrink-0', info.color)} />
                <span className="truncate font-mono flex-1">{file.path}</span>
                <span className="shrink-0 tabular-nums">
                  {file.additions > 0 && (
                    <span className="text-emerald-500">+{file.additions}</span>
                  )}
                  {file.additions > 0 && file.deletions > 0 && (
                    <span className="text-muted-foreground"> </span>
                  )}
                  {file.deletions > 0 && <span className="text-red-500">-{file.deletions}</span>}
                  {file.additions === 0 && file.deletions === 0 && (
                    <span className="text-muted-foreground">--</span>
                  )}
                </span>
              </div>
            )
          })}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
