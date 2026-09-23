import { useEffect, useState, type ReactNode } from 'react'
import { ChevronUp, Folder, Loader2 } from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { useSession } from '@droi/daemon-layer/use-session'
import { useTurn } from '@droi/daemon-layer/use-turn'
import { useSlashItems } from '@droi/daemon-layer/use-slash-items'
import { useContextUsage } from '@droi/daemon-layer/use-context-usage'
import { useGitChanges } from '@droi/daemon-layer/use-git-changes'
import { useSessionSettings } from '@droi/daemon-layer/use-session-settings'
import { COMPACT_COMMAND, useCompact } from '@droi/daemon-layer/use-compact'
import { usePrompts } from '@droi/daemon-layer/use-prompts'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { LOAD_STATE } from '@droi/daemon-layer/sdk-enums'
import { takePendingPrompt } from '@droi/daemon-layer/pending-prompt'
import { cn } from '@/lib/utils'
import { ComposerShelf, ContextMeter } from './composer-panels'
import { GitChangesButton } from './git-changes'
import { OpenInButton } from './open-in'
import { InputBar, type Submission } from './input-bar'
import { MessageList } from './message-list'
import { PromptArea } from './prompt-cards'
import { SessionSettingsBar, SessionTitle } from './session-toolbar'
import { COLUMN } from './column'

export { COLUMN } from './column'

export function SessionView({
  sessionId,
  title,
  workspace,
  tags,
  parent,
  onContinued,
  leading,
}: {
  sessionId: string
  title: string
  workspace: string | null
  tags: SessionSummary['tags']
  /** The Session this one continues after a compaction, when listed. */
  parent: Pick<SessionSummary, 'sessionId' | 'title'> | null
  /** `/compact` produced a child Session; the view should move there. */
  onContinued: (sessionId: string) => void
  leading?: ReactNode
}) {
  const session = useSession(sessionId)
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

  // The parent's transcript is only loaded once asked for; it can be large.
  const [showEarlier, setShowEarlier] = useState(false)
  const earlier = useSession(showEarlier && parent ? parent.sessionId : null)
  const lead = parent ? (
    <div className={cn(COLUMN, 'pb-2')}>
      {showEarlier ? (
        earlier.loadState !== LOAD_STATE.loaded ? (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 aria-hidden className="size-3.5 animate-spin" />
            Loading earlier messages…
          </p>
        ) : null
      ) : (
        <button
          type="button"
          onClick={() => setShowEarlier(true)}
          className="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          <ChevronUp aria-hidden className="size-3.5" />
          Continued from “{parent.title}” · Show earlier messages
        </button>
      )}
    </div>
  ) : null

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

  return (
    <section aria-label={title} className="flex h-full min-h-0 flex-col">
      <PageHeader leading={leading} title={<SessionTitle sessionId={sessionId} title={title} />}>
        <GitChangesButton changes={gitChanges} />
        <OpenInButton path={workspace} bridge={window.droiShell?.openIn ?? null} />
      </PageHeader>

      <div className="min-h-0 flex-1">
        {session.loadError ? (
          <p role="alert" className={cn(COLUMN, 'py-6 text-sm text-destructive-foreground')}>
            {session.loadError}
          </p>
        ) : !loaded && session.messages.length === 0 ? (
          <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 aria-hidden className="size-4 animate-spin" />
            Loading session…
          </div>
        ) : (
          <MessageList
            messages={session.messages}
            earlierMessages={showEarlier ? earlier.messages : undefined}
            workingState={
              compaction.isCompacting ? 'compacting_conversation' : session.workingState
            }
            lead={lead}
            scrollToEndKey={sentCount}
          />
        )}
      </div>

      <div className={cn(COLUMN, 'shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]')}>
        <ComposerShelf sessionId={sessionId} />
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
            footer={<SessionSettingsBar sessionId={sessionId} />}
            slashItems={slashItems}
            draftKey={sessionId}
          />
        )}
        <div className="flex h-7 items-center gap-3 px-2 text-xs text-muted-foreground">
          {workspace ? (
            <span className="flex min-w-0 items-center gap-1.5" title={workspace}>
              <Folder aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{workspaceName(workspace)}</span>
            </span>
          ) : null}
          <ContextMeter usage={contextUsage} />
        </div>
      </div>
    </section>
  )
}

function workspaceName(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}
