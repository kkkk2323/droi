import { Popover } from '@base-ui/react/popover'
import { GitBranch } from 'lucide-react'
import type { ChangedFile, GitChanges } from '@/daemon/use-git-changes'
import { cn } from '@/lib/utils'

/**
 * The Session's branch and uncommitted line counts, at the top right of the
 * header. Opens the changed-file list. Absent when the Workspace is not a
 * Git repository.
 */
export function GitChangesButton({ changes }: { changes: GitChanges | null }) {
  if (!changes) return null
  const dirty = changes.files.length > 0
  return (
    <Popover.Root>
      <Popover.Trigger
        aria-label={
          dirty
            ? `Branch ${changes.branch}, ${changes.files.length} changed ${plural(changes.files.length, 'file')}`
            : `Branch ${changes.branch}, no changes`
        }
        className="app-no-drag flex h-7 items-center gap-1.5 rounded-md px-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-accent data-[popup-open]:text-foreground"
      >
        <GitBranch aria-hidden className="size-3.5 shrink-0" />
        <span className="max-w-40 truncate">{changes.branch}</span>
        {dirty ? <Counts additions={changes.additions} deletions={changes.deletions} /> : null}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="end" sideOffset={6} className="z-50 outline-none">
          <Popover.Popup
            aria-label="Changed files"
            className="flex max-h-[min(24rem,var(--available-height))] w-[min(26rem,var(--available-width))] flex-col overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-xl outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none"
          >
            <Popover.Title className="flex items-center gap-2 px-3 pt-2.5 pb-1.5 text-xs text-muted-foreground">
              <span className="min-w-0 flex-1 truncate">
                {dirty
                  ? `${changes.files.length} uncommitted ${plural(changes.files.length, 'file')}`
                  : 'Working tree clean'}
              </span>
              {dirty ? <Counts additions={changes.additions} deletions={changes.deletions} /> : null}
            </Popover.Title>
            {dirty ? (
              <ul aria-label="Changed files" className="overflow-y-auto px-1.5 pb-1.5">
                {changes.files.map((file) => (
                  <FileRow key={file.path} file={file} />
                ))}
              </ul>
            ) : null}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

function Counts({ additions, deletions }: { additions: number; deletions: number }) {
  return (
    <span className="flex items-center gap-1 font-mono tabular-nums">
      <span className="text-emerald-600 dark:text-emerald-400">+{additions}</span>
      <span className="text-rose-600 dark:text-rose-400">−{deletions}</span>
    </span>
  )
}

const STATUS_LETTER: Record<string, string> = {
  added: 'A',
  modified: 'M',
  deleted: 'D',
  renamed: 'R',
  copied: 'C',
  untracked: 'A',
}

function FileRow({ file }: { file: ChangedFile }) {
  const slash = file.path.lastIndexOf('/')
  const dir = slash >= 0 ? file.path.slice(0, slash + 1) : ''
  const name = slash >= 0 ? file.path.slice(slash + 1) : file.path
  return (
    <li
      className="flex h-7 items-center gap-2 rounded-md px-1.5 text-xs"
      title={`${file.status}: ${file.path}`}
    >
      <span
        aria-label={file.status}
        className={cn(
          'w-3 shrink-0 text-center font-mono',
          file.status === 'deleted' ? 'text-rose-600 dark:text-rose-400' : 'text-muted-foreground',
        )}
      >
        {STATUS_LETTER[file.status] ?? file.status.charAt(0).toUpperCase()}
      </span>
      <span className="flex min-w-0 flex-1 items-baseline gap-1.5 font-mono">
        <span className="shrink-0">{name}</span>
        {dir ? <span className="truncate text-muted-foreground">{dir}</span> : null}
      </span>
      <Counts additions={file.additions} deletions={file.deletions} />
    </li>
  )
}

function plural(count: number, word: string): string {
  return count === 1 ? word : `${word}s`
}
