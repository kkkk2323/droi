import React, { useEffect, useMemo, useState } from 'react'
import { useParams, useNavigate } from '@tanstack/react-router'
import { useProjects, useProjectSettingsByRepo, useActions } from '@/store'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Separator } from '@/components/ui/separator'
import { Select, SelectContent, SelectItem, SelectTrigger } from '@/components/ui/select'
import { generateWorktreeBranch, sanitizeWorktreePrefix } from '@/lib/sessionWorktree'
import { useGitBranchesQuery } from '@/hooks/useGitStatus'

export function ProjectSettingsPage() {
  const projects = useProjects()
  const projectSettingsByRepo = useProjectSettingsByRepo()
  const { updateProjectSettings } = useActions()
  const params = useParams({ from: '/settings-layout/settings/projects/$projectDir' })
  const navigate = useNavigate()
  const selectedRepoRoot = decodeURIComponent(params.projectDir)
  const selectedProject = projects.find((p) => p.dir === selectedRepoRoot)

  const { data: rawBranches = [], isLoading: loadingBranches } = useGitBranchesQuery(selectedRepoRoot)
  const branches = useMemo(() => {
    return Array.from(new Set((rawBranches || []).filter(Boolean))).sort((a, b) => a.localeCompare(b))
  }, [rawBranches])

  const [baseBranch, setBaseBranch] = useState('')
  const [worktreePrefix, setWorktreePrefix] = useState('')
  const [setupScript, setSetupScript] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  const selectedSettings = useMemo(() => {
    if (!selectedRepoRoot) return {}
    return projectSettingsByRepo[selectedRepoRoot] || {}
  }, [projectSettingsByRepo, selectedRepoRoot])

  useEffect(() => {
    if (!selectedRepoRoot) return
    setBaseBranch(selectedSettings.baseBranch || '')
    setWorktreePrefix(selectedSettings.worktreePrefix || '')
    setSetupScript(selectedSettings.setupScript || '')
    setError('')
    setSaved(false)
  }, [selectedRepoRoot, selectedSettings.baseBranch, selectedSettings.worktreePrefix, selectedSettings.setupScript])

  const sampleBranch = useMemo(() => {
    const prefix = sanitizeWorktreePrefix(worktreePrefix) || 'droi'
    return generateWorktreeBranch(prefix)
  }, [worktreePrefix])

  const onSave = async () => {
    if (!selectedRepoRoot || saving) return
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      await updateProjectSettings(selectedRepoRoot, {
        baseBranch,
        worktreePrefix,
        setupScript,
      })
      setSaved(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (!selectedProject) {
    return (
      <div className="flex flex-1 flex-col overflow-auto">
        <div className="mx-auto w-full max-w-2xl space-y-8 p-8">
          <div>
            <h1 className="text-xl font-semibold">Project Not Found</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              The project &quot;{selectedRepoRoot}&quot; does not exist.
            </p>
          </div>
          <Button onClick={() => navigate({ to: '/settings' })}>
            Back to Settings
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="mx-auto w-full max-w-2xl space-y-8 p-8">
        <div>
          <h1 className="text-xl font-semibold">{selectedProject.name}</h1>
          <p className="mt-1 text-xs text-muted-foreground font-mono break-all">
            {selectedRepoRoot}
          </p>
        </div>

        <Separator />

        <div className="space-y-5">
          <div className="grid gap-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">Base Branch</label>
              <div className="text-xs text-muted-foreground">
                New sessions fork from this branch when creating a worktree.
              </div>
              <Select value={baseBranch} onValueChange={(v) => setBaseBranch(v || '')}>
                <SelectTrigger className="h-9">
                  {baseBranch || (loadingBranches ? 'Loading…' : 'Select base branch')}
                </SelectTrigger>
                <SelectContent>
                  {branches.map((b) => (
                    <SelectItem key={b} value={b}>{b}</SelectItem>
                  ))}
                  {!loadingBranches && branches.length === 0 && (
                    <div className="px-3 py-2 text-xs text-muted-foreground">No branches found</div>
                  )}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">Worktree Prefix</label>
              <div className="text-xs text-muted-foreground">
                New session branches will be created as <span className="font-mono">prefix/random-name</span>.
              </div>
              <Input
                value={worktreePrefix}
                onChange={(e) => setWorktreePrefix(e.currentTarget.value)}
                placeholder="droi"
                className="h-9 font-mono text-sm"
              />
              <div className="text-xs text-muted-foreground">
                Example: <span className="font-mono">{sampleBranch}</span>
              </div>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">Setup Script (Optional)</label>
              <div className="text-xs text-muted-foreground">
                Run automatically after creating a new session (macOS/Linux shell).
              </div>
              <Textarea
                value={setupScript}
                onChange={(e) => setSetupScript(e.currentTarget.value)}
                placeholder="npm install"
                className="min-h-[88px] font-mono text-sm"
              />
            </div>
          </div>

          {error && <div className="text-sm text-destructive-foreground">{error}</div>}
          {saved && <div className="text-sm text-emerald-600">Saved</div>}

          <div className="flex items-center gap-2">
            <Button onClick={onSave} disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

