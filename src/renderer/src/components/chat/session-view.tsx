import { useEffect, type ReactNode } from 'react'
import { Folder, Loader2 } from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { useSession } from '@/daemon/use-session'
import { useTurn } from '@/daemon/use-turn'
import { useSlashItems } from '@/daemon/use-slash-items'
import { useContextUsage } from '@/daemon/use-context-usage'
import { useSessionSettings } from '@/daemon/use-session-settings'
import { LOAD_STATE } from '@/daemon/sdk-enums'
import { takePendingPrompt } from '@/lib/pending-prompt'
import { cn } from '@/lib/utils'
import { ContextMeter, QueuedMessages, TodoPanel } from './composer-panels'
import { InputBar } from './input-bar'
import { MessageList } from './message-list'
import { PromptArea } from './prompt-cards'
import { ArchiveButton, SessionSettingsBar, SessionTitle } from './session-toolbar'
import { COLUMN } from './column'

export { COLUMN } from './column'

export function SessionView({
  sessionId,
  title,
  workspace,
  archived,
  onArchived,
  leading,
}: {
  sessionId: string
  title: string
  workspace: string | null
  archived: boolean
  onArchived: () => void
  leading?: ReactNode
}) {
  const session = useSession(sessionId)
  const turn = useTurn(sessionId)
  const slashItems = useSlashItems(sessionId)
  const isRunning = session.workingState !== 'idle'
  const loaded = session.loadState === LOAD_STATE.loaded
  const settings = useSessionSettings(sessionId)
  const contextUsage = useContextUsage(sessionId, {
    loaded,
    idle: !isRunning,
    modelId: settings.modelId,
  })

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
        <ArchiveButton sessionId={sessionId} archived={archived} onArchived={onArchived} />
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
          <MessageList messages={session.messages} workingState={session.workingState} />
        )}
      </div>

      <div className={cn(COLUMN, 'shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]')}>
        <PromptArea sessionId={sessionId} />
        <TodoPanel sessionId={sessionId} />
        <QueuedMessages sessionId={sessionId} />
        <InputBar
          isRunning={isRunning}
          disabled={!loaded}
          onSend={({ text, images, placement }) => void turn.send(text, { images, placement })}
          onCancel={() => void turn.cancel()}
          error={turn.sendError}
          footer={<SessionSettingsBar sessionId={sessionId} />}
          slashItems={slashItems}
        />
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
