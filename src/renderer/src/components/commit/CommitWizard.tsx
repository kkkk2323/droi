import React, { useEffect, useMemo, useReducer } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Loader2 } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'
import { useGitStatusQuery, useGitBranchQuery, useGitBranchesQuery } from '@/hooks/useGitStatus'
import { CommitReviewStep } from './CommitReviewStep'
import { CommitMessageStep } from './CommitMessageStep'
import { WorkflowOptionsStep } from './WorkflowOptionsStep'
import { getStepLabel } from './CommitProgressView'
import type { StepState } from './CommitProgressView'
import type { CommitWorkflow, GitToolsInfo, GenerateCommitMetaResult, WorkflowStepName, WorkflowStepProgress } from '@/types'

// --- State machine ---

type Phase = 'configure' | 'executing' | 'done' | 'error'

interface WizardState {
  phase: Phase
  includeUnstaged: boolean
  commitMessage: string
  commitWorkflow: CommitWorkflow
  prBaseBranch: string
  mergeEnabled: boolean
  mergeBranch: string
  generating: boolean
  gitTools: GitToolsInfo
  generatedPrMeta: { title: string; body: string } | null
  steps: StepState[]
  commitResult: { commitHash: string; prUrl?: string } | null
  error: string
}

type WizardAction =
  | { type: 'reset' }
  | { type: 'set_include_unstaged'; value: boolean }
  | { type: 'set_commit_message'; value: string }
  | { type: 'set_workflow'; value: CommitWorkflow }
  | { type: 'set_pr_base_branch'; value: string }
  | { type: 'set_merge_enabled'; value: boolean }
  | { type: 'set_merge_branch'; value: string }
  | { type: 'set_generating'; value: boolean }
  | { type: 'set_git_tools'; value: GitToolsInfo }
  | { type: 'set_generated_pr_meta'; value: { title: string; body: string } | null }
  | { type: 'set_commit_message_and_pr'; message: string; prMeta: { title: string; body: string } | null }
  | { type: 'start_executing'; steps: StepState[] }
  | { type: 'step_progress'; progress: WorkflowStepProgress }
  | { type: 'set_done'; result: { commitHash: string; prUrl?: string } }
  | { type: 'set_error'; error: string }

const initialState: WizardState = {
  phase: 'configure',
  includeUnstaged: true,
  commitMessage: '',
  commitWorkflow: 'commit',
  prBaseBranch: '',
  mergeEnabled: false,
  mergeBranch: '',
  generating: false,
  gitTools: { hasGh: false, hasFlow: false, prTool: null },
  generatedPrMeta: null,
  steps: [],
  commitResult: null,
  error: '',
}

function reducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case 'reset':
      return initialState
    case 'set_include_unstaged':
      return { ...state, includeUnstaged: action.value }
    case 'set_commit_message':
      return { ...state, commitMessage: action.value }
    case 'set_workflow':
      return { ...state, commitWorkflow: action.value, generatedPrMeta: action.value !== 'commit_push_pr' ? null : state.generatedPrMeta }
    case 'set_pr_base_branch':
      return { ...state, prBaseBranch: action.value }
    case 'set_merge_enabled':
      return { ...state, mergeEnabled: action.value }
    case 'set_merge_branch':
      return { ...state, mergeBranch: action.value }
    case 'set_generating':
      return { ...state, generating: action.value }
    case 'set_git_tools':
      return { ...state, gitTools: action.value }
    case 'set_generated_pr_meta':
      return { ...state, generatedPrMeta: action.value }
    case 'set_commit_message_and_pr':
      return { ...state, commitMessage: action.message, generatedPrMeta: action.prMeta, generating: false }
    case 'start_executing':
      return { ...state, phase: 'executing', steps: action.steps, error: '', commitResult: null }
    case 'step_progress': {
      const steps = state.steps.map((s) =>
        s.step === action.progress.step
          ? { ...s, status: action.progress.status, detail: action.progress.detail }
          : s
      )
      return { ...state, steps }
    }
    case 'set_done':
      return { ...state, phase: 'done', commitResult: action.result }
    case 'set_error':
      return { ...state, phase: 'error', error: action.error }
    default:
      return state
  }
}

// --- Component ---

interface CommitWizardProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  projectDir: string
}

export function CommitWizard({ open, onOpenChange, projectDir }: CommitWizardProps) {
  const [state, dispatch] = useReducer(reducer, initialState)
  const queryClient = useQueryClient()

  const { data: gitFiles = [], isLoading: statusLoading } = useGitStatusQuery(projectDir, open)
  const { data: branch = '', isLoading: branchLoading } = useGitBranchQuery(projectDir, open)
  const { data: allBranches = [], isLoading: branchesLoading } = useGitBranchesQuery(projectDir, open)
  const loading = statusLoading || branchLoading || branchesLoading

  const localBranches = useMemo(() => allBranches.filter((b) => b !== branch), [allBranches, branch])
  const stagedFiles = useMemo(() => gitFiles.filter((f) => f.status !== '??'), [gitFiles])
  const unstagedFiles = useMemo(() => gitFiles.filter((f) => f.status === '??'), [gitFiles])
  const filesToCommit = state.includeUnstaged ? gitFiles : stagedFiles

  const defaultPrBaseBranch = useMemo(() => {
    return localBranches.includes('main') ? 'main' : (localBranches[0] || '')
  }, [localBranches])

  // Reset state when dialog opens
  useEffect(() => {
    if (!open) return
    dispatch({ type: 'reset' })
    void getDroidClient().detectGitTools({ projectDir })
      .then((tools) => dispatch({ type: 'set_git_tools', value: tools }))
      .catch(() => dispatch({ type: 'set_git_tools', value: { hasGh: false, hasFlow: false, prTool: null } }))
  }, [open, projectDir])

  // Set default PR base branch
  useEffect(() => {
    if (!open || state.prBaseBranch) return
    if (defaultPrBaseBranch) dispatch({ type: 'set_pr_base_branch', value: defaultPrBaseBranch })
  }, [open, state.prBaseBranch, defaultPrBaseBranch])

  // If PR tool unavailable, downgrade workflow
  useEffect(() => {
    if (state.commitWorkflow === 'commit_push_pr' && !state.gitTools.prTool) {
      dispatch({ type: 'set_workflow', value: 'commit_push' })
    }
  }, [state.commitWorkflow, state.gitTools.prTool])

  const requiresPrBaseBranch = state.commitWorkflow === 'commit_push_pr'

  const commitActionLabel = state.commitWorkflow === 'commit'
    ? 'Commit'
    : state.commitWorkflow === 'commit_push'
      ? 'Commit & Push'
      : 'Commit, Push & Create PR'

  const commitActionDisabled = loading
    || state.phase === 'executing'
    || state.generating
    || filesToCommit.length === 0
    || (requiresPrBaseBranch && !state.prBaseBranch)
    || (state.mergeEnabled && !state.mergeBranch)

  const handleGenerate = async () => {
    if (state.generating || filesToCommit.length === 0) return
    dispatch({ type: 'set_generating', value: true })
    try {
      const wantPrMeta = state.commitWorkflow === 'commit_push_pr'
      const res: GenerateCommitMetaResult = await getDroidClient().generateCommitMeta({
        projectDir,
        includeUnstaged: state.includeUnstaged,
        wantPrMeta,
        prBaseBranch: wantPrMeta ? state.prBaseBranch : undefined,
      })
      dispatch({
        type: 'set_commit_message_and_pr',
        message: res.commitMessage || '',
        prMeta: wantPrMeta ? { title: res.prTitle || '', body: res.prBody || '' } : null,
      })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      dispatch({ type: 'set_error', error: msg || 'Failed to generate message' })
      dispatch({ type: 'set_generating', value: false })
    }
  }

  const buildSteps = (): StepState[] => {
    const steps: StepState[] = []
    if (state.includeUnstaged) {
      steps.push({ step: 'stage', label: getStepLabel('stage'), status: 'pending' })
    }
    steps.push({ step: 'commit', label: getStepLabel('commit'), status: 'pending' })
    if (state.mergeEnabled && state.mergeBranch) {
      steps.push({ step: 'merge', label: getStepLabel('merge'), status: 'pending' })
    }
    if (state.commitWorkflow === 'commit_push' || state.commitWorkflow === 'commit_push_pr') {
      steps.push({ step: 'push', label: getStepLabel('push'), status: 'pending' })
    }
    if (state.commitWorkflow === 'commit_push_pr') {
      steps.push({ step: 'create_pr', label: getStepLabel('create_pr'), status: 'pending' })
    }
    return steps
  }

  const invalidateGitQueries = () => {
    void queryClient.invalidateQueries({ queryKey: ['gitStatus', projectDir] })
    void queryClient.invalidateQueries({ queryKey: ['gitBranch', projectDir] })
    void queryClient.invalidateQueries({ queryKey: ['gitBranches', projectDir] })
  }

  const handleCommit = async () => {
    if (filesToCommit.length === 0 || state.phase === 'executing') return
    const wantPrMeta = state.commitWorkflow === 'commit_push_pr'
    if (wantPrMeta && !state.gitTools.prTool) {
      dispatch({ type: 'set_error', error: state.gitTools.prDisabledReason || 'PR creation is not available on this machine.' })
      return
    }

    let message = state.commitMessage.trim()
    let prMeta = state.generatedPrMeta

    // Auto-generate if no message or need PR meta
    if (!message || (wantPrMeta && !prMeta)) {
      dispatch({ type: 'set_generating', value: true })
      try {
        const res = await getDroidClient().generateCommitMeta({
          projectDir,
          includeUnstaged: state.includeUnstaged,
          wantPrMeta,
          prBaseBranch: wantPrMeta ? state.prBaseBranch : undefined,
        })
        if (!message) message = res.commitMessage.trim()
        if (wantPrMeta) prMeta = { title: res.prTitle || '', body: res.prBody || '' }
        dispatch({
          type: 'set_commit_message_and_pr',
          message,
          prMeta: wantPrMeta ? prMeta : null,
        })
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        dispatch({ type: 'set_error', error: msg || 'Failed to generate message' })
        dispatch({ type: 'set_generating', value: false })
        return
      }
    }

    if (!message) {
      dispatch({ type: 'set_error', error: 'Commit message is empty' })
      return
    }

    // Switch to progress view
    const steps = buildSteps()
    dispatch({ type: 'start_executing', steps })

    // Subscribe to progress events
    const unsub = getDroidClient().onCommitWorkflowProgress((progress) => {
      dispatch({ type: 'step_progress', progress })
    })

    try {
      const res = await getDroidClient().commitWorkflow({
        projectDir,
        includeUnstaged: state.includeUnstaged,
        commitMessage: message,
        workflow: state.commitWorkflow,
        prBaseBranch: wantPrMeta ? state.prBaseBranch : undefined,
        prTitle: wantPrMeta ? (prMeta?.title?.trim() || undefined) : undefined,
        prBody: wantPrMeta ? (prMeta?.body || '') : undefined,
        mergeEnabled: state.mergeEnabled,
        mergeBranch: state.mergeEnabled ? state.mergeBranch : undefined,
      })
      dispatch({ type: 'set_done', result: { commitHash: res.commitHash, prUrl: res.prUrl } })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      dispatch({ type: 'set_error', error: msg || 'Failed to commit' })
    } finally {
      unsub()
      invalidateGitQueries()
    }
  }

  const isFinished = state.phase === 'done' || state.phase === 'error'
  const isLocked = state.phase !== 'configure' || state.generating

  // Step groups for each collapsible section
  const reviewSteps = useMemo(() => state.steps.filter((s) => s.step === 'stage'), [state.steps])
  const messageSteps = useMemo(() => state.steps.filter((s) => s.step === 'commit'), [state.steps])
  const workflowSteps = useMemo(() => state.steps.filter((s) => s.step === 'merge' || s.step === 'push' || s.step === 'create_pr'), [state.steps])

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>Git Commit</AlertDialogTitle>
          <AlertDialogDescription>
            {state.phase === 'configure'
              ? (state.generating
                ? 'Generating commit message…'
                : 'Review changes, configure workflow, then commit.')
              : state.phase === 'executing'
                ? 'Running workflow…'
                : state.phase === 'done'
                  ? 'Workflow completed successfully.'
                  : 'Workflow encountered an error.'}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {loading ? (
          <div className="flex items-center justify-center py-6">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="space-y-1 divide-y divide-border min-w-0">
            <CommitReviewStep
              branch={branch}
              filesToCommit={filesToCommit}
              unstagedCount={unstagedFiles.length}
              includeUnstaged={state.includeUnstaged}
              onIncludeUnstagedChange={(v) => dispatch({ type: 'set_include_unstaged', value: v })}
              disabled={state.generating}
              executingSteps={reviewSteps}
              locked={isLocked}
            />
            <CommitMessageStep
              commitMessage={state.commitMessage}
              onCommitMessageChange={(v) => dispatch({ type: 'set_commit_message', value: v })}
              onGenerate={handleGenerate}
              generating={state.generating}
              disabled={false}
              hasFiles={filesToCommit.length > 0}
              executingSteps={messageSteps}
              locked={isLocked}
            />
            <WorkflowOptionsStep
              commitWorkflow={state.commitWorkflow}
              onWorkflowChange={(v) => dispatch({ type: 'set_workflow', value: v })}
              gitTools={state.gitTools}
              prBaseBranch={state.prBaseBranch}
              onPrBaseBranchChange={(v) => dispatch({ type: 'set_pr_base_branch', value: v })}
              mergeEnabled={state.mergeEnabled}
              onMergeEnabledChange={(v) => dispatch({ type: 'set_merge_enabled', value: v })}
              mergeBranch={state.mergeBranch}
              onMergeBranchChange={(v) => dispatch({ type: 'set_merge_branch', value: v })}
              localBranches={localBranches}
              disabled={state.generating}
              executingSteps={workflowSteps}
              locked={isLocked}
            />
            {state.error && (
              <div className="px-3 pt-2 text-xs text-destructive-foreground">{state.error}</div>
            )}
            {state.commitResult && (
              <div className="px-3 pt-2 text-xs text-emerald-600">
                Committed <span className="font-mono">{state.commitResult.commitHash.slice(0, 7)}</span>
                {state.commitResult.prUrl && (
                  <>
                    {' '}&middot; PR:{' '}
                    {/^https?:\/\//.test(state.commitResult.prUrl)
                      ? <a className="underline" href={state.commitResult.prUrl} target="_blank" rel="noreferrer">{state.commitResult.prUrl}</a>
                      : <span className="font-mono">{state.commitResult.prUrl}</span>}
                  </>
                )}
              </div>
            )}
          </div>
        )}

        <AlertDialogFooter>
          {state.phase === 'configure' && !state.generating && (
            <>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                disabled={commitActionDisabled}
                onClick={() => { void handleCommit() }}
              >
                {commitActionLabel}
              </AlertDialogAction>
            </>
          )}
          {state.phase === 'configure' && state.generating && (
            <AlertDialogCancel disabled>Generating…</AlertDialogCancel>
          )}
          {state.phase === 'executing' && (
            <AlertDialogCancel disabled>Running…</AlertDialogCancel>
          )}
          {isFinished && (
            <>
              {state.phase === 'error' && (
                <AlertDialogAction onClick={() => dispatch({ type: 'reset' })}>
                  Retry
                </AlertDialogAction>
              )}
              <AlertDialogCancel>Close</AlertDialogCancel>
            </>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
