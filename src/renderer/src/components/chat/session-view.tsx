import { useEffect, type ReactNode } from 'react'
import { Folder, Loader2 } from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { useSession } from '@/daemon/use-session'
import { useTurn } from '@/daemon/use-turn'
import { LOAD_STATE, type LoadState } from '@/daemon/sdk-enums'
import { takePendingPrompt } from '@/lib/pending-prompt'
import { cn } from '@/lib/utils'
import { InputBar } from './input-bar'
import { MessageList } from './message-list'
import { PromptArea } from './prompt-cards'
import { ArchiveButton, SessionSettingsBar, SessionTitle } from './session-toolbar'

/** Transcript, Prompts and composer share one reading column. */
export const COLUMN = 'mx-auto w-full max-w-3xl px-4 md:px-6'

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
  const isRunning = session.workingState !== 'idle'
  const loaded = session.loadState === LOAD_STATE.loaded

  // A message typed on the New session page goes out as soon as the Session can take it.
  const send = turn.send
  useEffect(() => {
    if (!loaded) return
    const text = takePendingPrompt(sessionId)
    if (text) void send(text)
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
          <MessageList
            messages={session.messages}
            isStreaming={session.workingState === 'streaming_assistant_message'}
          />
        )}
      </div>

      <div className={cn(COLUMN, 'shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]')}>
        <PromptArea sessionId={sessionId} />
        <InputBar
          isRunning={isRunning}
          disabled={!loaded}
          onSend={(text) => void turn.send(text)}
          onCancel={() => void turn.cancel()}
          error={turn.sendError}
          footer={<SessionSettingsBar sessionId={sessionId} />}
        />
        <div className="flex h-7 items-center gap-3 px-2 text-xs text-muted-foreground">
          {workspace ? (
            <span className="flex min-w-0 items-center gap-1.5" title={workspace}>
              <Folder aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{workspaceName(workspace)}</span>
            </span>
          ) : null}
          <WorkingState state={session.workingState} loadState={session.loadState} />
        </div>
      </div>
    </section>
  )
}

const WORKING_LABELS: Record<string, string> = {
  idle: '',
  thinking: 'Thinking',
  streaming_assistant_message: 'Responding',
  waiting_for_tool_confirmation: 'Waiting for your approval',
  executing_tool: 'Running a tool',
  compacting_conversation: 'Compacting',
}

function WorkingState({ state, loadState }: { state: string; loadState: LoadState }) {
  const label = loadState === LOAD_STATE.loaded ? (WORKING_LABELS[state] ?? state) : ''
  return (
    <div
      role="status"
      aria-label="Session activity"
      className="ml-auto flex shrink-0 items-center gap-1.5 text-right"
    >
      {label ? <Loader2 aria-hidden className="size-3 animate-spin" /> : null}
      {label}
    </div>
  )
}

function workspaceName(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}
