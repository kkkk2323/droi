import { useSession } from '@/daemon/use-session'
import { LOAD_STATE, type LoadState } from '@/daemon/sdk-enums'
import { MessageList } from './message-list'

export function SessionView({ sessionId, title }: { sessionId: string; title: string }) {
  const session = useSession(sessionId)

  return (
    <section aria-label={title} className="flex h-full min-h-0 flex-col">
      <header className="flex h-10 shrink-0 items-center gap-2 border-b px-4">
        <h2 className="truncate text-sm font-medium">{title}</h2>
        <WorkingState state={session.workingState} loadState={session.loadState} />
      </header>
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
            streamingMessageIds={session.streamingMessageIds}
          />
        )}
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
    <span
      role="status"
      aria-label="Session activity"
      className="ml-auto text-xs text-muted-foreground"
    >
      {label}
    </span>
  )
}
