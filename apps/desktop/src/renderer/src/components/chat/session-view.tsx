import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ChevronUp, Folder } from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/page-header'
import { useSession, useSessions } from '@droi/daemon-layer/use-session'
import { useTurn } from '@droi/daemon-layer/use-turn'
import { useSlashItems } from '@droi/daemon-layer/use-slash-items'
import { useContextUsage } from '@droi/daemon-layer/use-context-usage'
import { useGitChanges } from '@droi/daemon-layer/use-git-changes'
import { useSessionSettings } from '@droi/daemon-layer/use-session-settings'
import { COMPACT_COMMAND, useCompact } from '@droi/daemon-layer/use-compact'
import { usePrompts } from '@droi/daemon-layer/use-prompts'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { useListNewSubagents, type SessionRef } from '@droi/daemon-layer/subagents'
import { LOAD_STATE } from '@droi/daemon-layer/sdk-enums'
import { takePendingPrompt } from '@droi/daemon-layer/pending-prompt'
import { ImageSessionProvider } from '@droi/daemon-layer/local-image'
import { useOlderMessages } from '@droi/daemon-layer/use-older-messages'
import type { QueuedContent } from '@droi/daemon-layer/use-queued-messages'
import { loadDraft, saveDraft } from '@droi/daemon-layer/drafts'
import { cn } from '@/lib/utils'
import { CompactedNotice, ComposerShelf, ContextMeter } from './composer-panels'
import { CopySessionMenu } from './copy-session-menu'
import { GitChangesButton } from './git-changes'
import { OpenInButton } from './open-in'
import { InputBar, type InputBarHandle, type Submission } from './input-bar'
import { MessageList } from './message-list'
import { PromptArea } from './prompt-cards'
import { SessionSettingsBar, SessionTitle } from './session-toolbar'
import { ToolsButton } from './tools-panel'
import { SessionTrail, SubagentMenu } from './subagent-nav'
import { COLUMN } from './column'

export { COLUMN } from './column'

export function SessionView({
  sessionId,
  title,
  workspace,
  tags,
  chain,
  trail,
  siblings,
  subagents,
  onContinued,
  leading,
}: {
  sessionId: string
  title: string
  workspace: string | null
  tags: SessionSummary['tags']
  /** The listed Sessions this one continues after compactions, nearest first. */
  chain: readonly Pick<SessionSummary, 'sessionId' | 'title'>[]
  /** For a subagent: the Sessions above it, its main Session first. */
  trail: readonly SessionRef[]
  /** For a subagent: every subagent of the same caller, itself included. */
  siblings: readonly SessionSummary[]
  /** The subagents this Session (or an earlier link of its chain) called. */
  subagents: readonly SessionSummary[]
  /** `/compact` produced a child Session; the view should move there. */
  onContinued: (sessionId: string) => void
  leading?: ReactNode
}) {
  const session = useSession(sessionId)
  useListNewSubagents(session.transcript)
  const turn = useTurn(sessionId)
  const slashItems = useSlashItems(sessionId)
  const compaction = useCompact(sessionId, tags)
  const isRunning = session.workingState !== 'idle' || compaction.isCompacting
  const loaded = session.loadState === LOAD_STATE.loaded
  const settings = useSessionSettings(sessionId)
  const prompts = usePrompts(sessionId)
  const hasPrompt = prompts.permissions.length > 0 || prompts.askUser.length > 0
  const contextUsage = useContextUsage(sessionId, { loaded, modelId: settings.modelId })
  const gitChanges = useGitChanges(sessionId, { loaded, running: isRunning })
  const older = useOlderMessages(sessionId)

  // Earlier Sessions load one per click, nearest first; each can be large.
  const [revealed, setRevealed] = useState(0)
  const shown = chain.slice(0, revealed).reverse()
  const earlierViews = useSessions(shown.map((s) => s.sessionId))
  const earlier = useMemo(() => earlierViews.map((v) => v.transcript), [earlierViews])
  const nextEarlier = chain[revealed]
  const earlierError = earlierViews.find((v) => v.loadError)?.loadError
  const loadingEarlier = earlierViews.some((v) => v.loadState !== LOAD_STATE.loaded)
  const lead =
    earlierError || loadingEarlier || nextEarlier || session.hasOlderMessages ? (
      <div className={cn(COLUMN, 'pb-2')}>
        {session.hasOlderMessages ? (
          older.error ? (
            <p role="alert" className="text-xs text-destructive-foreground">
              Previous messages did not load: {older.error}
            </p>
          ) : (
            <button
              type="button"
              disabled={older.isLoading}
              onClick={() => void older.load()}
              className="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            >
              {older.isLoading ? (
                <Spinner aria-hidden className="size-3.5" />
              ) : (
                <ChevronUp aria-hidden className="size-3.5" />
              )}
              {older.isLoading ? 'Loading previous messages…' : 'Load previous messages'}
            </button>
          )
        ) : null}
        {earlierError ? (
          <p role="alert" className="text-xs text-destructive-foreground">
            Earlier messages did not load: {earlierError}
          </p>
        ) : loadingEarlier ? (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <Spinner aria-hidden className="size-3.5" />
            Loading earlier messages…
          </p>
        ) : nextEarlier ? (
          <button
            type="button"
            onClick={() => setRevealed(revealed + 1)}
            className="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            <ChevronUp aria-hidden className="size-3.5" />
            Continued from “{nextEarlier.title}” · Show earlier messages
          </button>
        ) : null}
      </div>
    ) : null

  // A paused queued message taken back goes into the composer; while a Prompt
  // stands in for the composer it waits in the draft instead.
  const composer = useRef<InputBarHandle>(null)
  const takeBack = (content: QueuedContent) => {
    if (composer.current) {
      composer.current.insert(content)
      return
    }
    const draft = loadDraft(sessionId)
    saveDraft(sessionId, {
      text: draft.text.trim() ? `${content.text}\n${draft.text}` : content.text,
      images: [...content.images, ...draft.images],
    })
  }

  const [sentCount, setSentCount] = useState(0)
  const submit = ({ text, images, placement }: Submission) => {
    setSentCount((n) => n + 1)
    const command = COMPACT_COMMAND.exec(text.trim())
    if (command && images.length === 0) {
      void compaction.compact(command[1]).then((next) => {
        if (next) onContinued(next)
      })
      return
    }
    void turn.send(text, { images, placement })
  }

  // A message typed on the New session page goes out as soon as the Session can take it.
  const send = turn.send
  useEffect(() => {
    if (!loaded) return
    const prompt = takePendingPrompt(sessionId)
    if (prompt) void send(prompt.text, { images: prompt.images })
  }, [loaded, sessionId, send])

  // Escape interrupts the running turn, as in the Factory App, unless something
  // on top (a dialog, a menu, the slash suggestions) is using the key itself.
  const cancel = turn.cancel
  const canInterrupt = isRunning && !hasPrompt
  useEffect(() => {
    if (!canInterrupt) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing) return
      if (document.querySelector('[role="dialog"], [role="menu"], [role="listbox"]')) return
      event.preventDefault()
      void cancel()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [canInterrupt, cancel])

  return (
    <section aria-label={title} className="flex h-full min-h-0 flex-col">
      <PageHeader
        leading={leading}
        title={
          trail.length > 0 ? (
            <SessionTrail sessionId={sessionId} title={title} trail={trail} siblings={siblings} />
          ) : (
            <SessionTitle sessionId={sessionId} title={title} />
          )
        }
      >
        <SubagentMenu subagents={subagents} />
        <GitChangesButton changes={gitChanges} />
        <CopySessionMenu sessionId={sessionId} title={title} workspace={workspace} />
        <OpenInButton path={workspace} bridge={window.droiShell?.openIn ?? null} />
      </PageHeader>

      <div className="min-h-0 flex-1">
        {session.loadError ? (
          <p role="alert" className={cn(COLUMN, 'py-6 text-sm text-destructive-foreground')}>
            {session.loadError}
          </p>
        ) : !loaded && session.messages.length === 0 ? (
          <div className="appear-after-wait flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
            <Spinner aria-hidden className="size-4" />
            Loading session…
          </div>
        ) : (
          <ImageSessionProvider value={sessionId}>
            <MessageList
              transcript={session.transcript}
              earlier={earlier}
              workingState={
                compaction.isCompacting ? 'compacting_conversation' : session.workingState
              }
              lead={lead}
              scrollToEndKey={sentCount}
              stateKey={sessionId}
            />
          </ImageSessionProvider>
        )}
      </div>

      <div className={cn(COLUMN, 'shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]')}>
        <ComposerShelf sessionId={sessionId} onTake={takeBack} />
        {hasPrompt ? (
          // The question or permission takes the composer's place; the draft
          // is kept and comes back with the composer once answered.
          <PromptArea sessionId={sessionId} />
        ) : (
          <InputBar
            isRunning={isRunning}
            disabled={!loaded}
            onSend={submit}
            onCancel={() => void turn.cancel()}
            error={turn.sendError ?? compaction.error}
            footer={
              <>
                <SessionSettingsBar sessionId={sessionId} />
                <ToolsButton sessionId={sessionId} />
              </>
            }
            slashItems={slashItems}
            draftKey={sessionId}
            ref={composer}
          />
        )}
        <div className="flex h-7 items-center gap-3 px-2 text-xs text-muted-foreground">
          {workspace ? (
            <span className="flex min-w-0 items-center gap-1.5" title={workspace}>
              <Folder aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{workspaceName(workspace)}</span>
            </span>
          ) : null}
          <CompactedNotice sessionId={sessionId} />
          <ContextMeter usage={contextUsage} />
        </div>
      </div>
    </section>
  )
}

function workspaceName(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}
