import React, { useMemo, useState } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Upload, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { getDroidClient } from '@/droidClient'
import { useProjects, useActiveSessionId } from '@/store'
import { useGitBranchQuery } from '@/hooks/useGitStatus'

interface PushDialogProps {
  projectDir: string
  isRunning: boolean
}

export function PushDialog({ projectDir, isRunning }: PushDialogProps) {
  const projects = useProjects()
  const activeSessionId = useActiveSessionId()
  const [open, setOpen] = useState(false)
  const [pushing, setPushing] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState('')

  const branch = useMemo(() => {
    for (const p of projects) {
      const s = p.sessions.find((x) => x.id === activeSessionId)
      if (s?.branch) return s.branch
    }
    return ''
  }, [projects, activeSessionId])

  const { data: resolvedBranch = '' } = useGitBranchQuery(projectDir, open)

  const effectiveBranch = resolvedBranch || branch
  const disabled = !projectDir || isRunning || !effectiveBranch

  const onPush = async () => {
    if (!projectDir || pushing) return
    setPushing(true)
    setError('')
    setResult('')
    try {
      const res = await getDroidClient().pushBranch({ projectDir })
      setResult(`Pushed ${res.branch} to ${res.remote}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setPushing(false)
    }
  }

  if (!projectDir) return null

  return (
    <AlertDialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) { setError(''); setResult('') } }}>
      <AlertDialogTrigger
        render={<button type="button" />}
        className={cn(
          'flex items-center gap-1.5 rounded-md border border-border bg-card/80 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent',
          disabled && 'pointer-events-none opacity-50'
        )}
        disabled={disabled}
      >
        <Upload className="size-3.5" />
        <span>Push</span>
      </AlertDialogTrigger>

      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Git Push</AlertDialogTitle>
          <AlertDialogDescription>
            Push this worktree branch to <span className="font-mono">origin</span> and set upstream.
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="space-y-2">
          <div className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
            Branch: <span className="font-mono text-foreground">{effectiveBranch || 'unknown'}</span>
          </div>
          {error && <div className="text-xs text-red-500">{error}</div>}
          {result && <div className="text-xs text-emerald-600">{result}</div>}
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={pushing}>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={pushing || !effectiveBranch} onClick={onPush}>
            {pushing ? (
              <>
                <Loader2 className="mr-2 size-3.5 animate-spin" />
                Pushing…
              </>
            ) : (
              'Push'
            )}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
