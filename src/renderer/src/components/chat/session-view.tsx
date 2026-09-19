import { useSession } from '@/daemon/use-session'
import { cn } from '@/lib/utils'
import { useTurn } from '@/daemon/use-turn'
import { LOAD_STATE, type LoadState } from '@/daemon/sdk-enums'
import { InputBar } from './input-bar'
import { MessageList } from './message-list'
import { PromptArea } from './prompt-cards'
import { SessionToolbar } from './session-toolbar'

export function SessionView({
  sessionId,
  title,
  archived,
  onArchived,
}: {
  sessionId: string
  title: string
  archived: boolean
  onArchived: () => void
}) {
  const session = useSession(sessionId)
  const turn = useTurn(sessionId)
  const isRunning = session.workingState !== 'idle'

  return (
    <section aria-label={title} className="flex h-full min-h-0 flex-col">
      <SessionToolbar
        sessionId={sessionId}
        title={title}
        archived={archived}
        onArchived={onArchived}
      />
      <WorkingState state={session.workingState} loadState={session.loadState} />
      <div className="min-h-0 flex-1">
        {session.loadError ? (
          <p role="alert" className="p-4 text-sm text-destructive-foreground">
            {session.loadError}
          </p>
        ) : session.loadState !== LOAD_STATE.loaded && session.messages.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Loading session…
          </div>
        ) : (
          <MessageList
            messages={session.messages}
            isStreaming={session.workingState === 'streaming_assistant_message'}
          />
        )}
      </div>
      <PromptArea sessionId={sessionId} />
      <InputBar
        isRunning={isRunning}
        disabled={session.loadState !== LOAD_STATE.loaded}
        onSend={(text) => void turn.send(text)}
        onCancel={() => void turn.cancel()}
        error={turn.sendError}
      />
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
      className={cn(
        'shrink-0 px-4 text-xs text-muted-foreground',
        label ? 'py-1' : 'h-0 overflow-hidden',
      )}
    >
      {label}
    </div>
  )
}
