import { Menu } from '@base-ui/react/menu'
import { Bot, Check, ChevronDown, ChevronRight, CircleDashed } from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import {
  isRunning,
  useSubagentLinks,
  type SessionRef,
  type SubagentRun,
} from '@droi/daemon-layer/subagents'
import { relativeTime } from '@/components/sidebar/session-sidebar'

const POPUP =
  'max-h-[min(24rem,var(--available-height))] w-80 max-w-[calc(100vw-1rem)] overflow-y-auto rounded-lg border bg-popover p-1 text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none'
const ITEM =
  'flex items-center gap-2 rounded-md py-1.5 pr-2 pl-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground'

/** The header's way into a Session's subagents: how many, how many still running. */
export function SubagentMenu({ subagents }: { subagents: readonly SessionSummary[] }) {
  const links = useSubagentLinks()
  if (!links || subagents.length === 0) return null
  const running = subagents.filter((s) => isRunning(links.runs.get(s.sessionId)?.status)).length
  const count = `${subagents.length} ${subagents.length === 1 ? 'subagent' : 'subagents'}`
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label={running > 0 ? `${count}, ${running} running` : count}
        className="app-no-drag flex h-7 items-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-accent"
      >
        {running > 0 ? (
          <Spinner aria-hidden className="size-3.5 text-sky-600 dark:text-sky-400" />
        ) : (
          <Bot aria-hidden className="size-3.5" />
        )}
        <span className="hidden sm:inline">{count}</span>
        <span className="sm:hidden">{subagents.length}</span>
        <ChevronDown aria-hidden className="size-3" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner side="bottom" align="end" sideOffset={6} className="z-50 outline-none">
          <Menu.Popup aria-label="Subagents" className={POPUP}>
            {subagents.map((s) => (
              <Menu.Item key={s.sessionId} onClick={() => links.open(s.sessionId)} className={ITEM}>
                <SubagentRow session={s} run={links.runs.get(s.sessionId)} />
              </Menu.Item>
            ))}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}

/**
 * A subagent's title as a trail: the Sessions above it lead back, and the
 * last crumb switches to another subagent of the same caller.
 */
export function SessionTrail({
  sessionId,
  title,
  trail,
  siblings,
}: {
  sessionId: string
  title: string
  trail: readonly SessionRef[]
  siblings: readonly SessionSummary[]
}) {
  const links = useSubagentLinks()
  return (
    <nav aria-label="Session hierarchy" className="flex min-w-0 items-center gap-0.5">
      {trail.map((crumb) => (
        <span key={crumb.sessionId} className="flex min-w-0 shrink items-center gap-0.5">
          <button
            type="button"
            onClick={() => links?.open(crumb.sessionId)}
            className="app-no-drag max-w-48 truncate rounded-md px-1.5 py-0.5 text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            {crumb.title}
          </button>
          <ChevronRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground/60" />
        </span>
      ))}
      <Menu.Root>
        <Menu.Trigger className="app-no-drag flex min-w-0 items-center gap-1 rounded-md px-1.5 py-0.5 outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-accent">
          <span className="truncate">{title}</span>
          <ChevronDown aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner side="bottom" align="start" sideOffset={6} className="z-50 outline-none">
            <Menu.Popup aria-label="Subagents" className={POPUP}>
              <Menu.RadioGroup value={sessionId}>
                {siblings.map((s) => (
                  <Menu.RadioItem
                    key={s.sessionId}
                    value={s.sessionId}
                    closeOnClick
                    onClick={() => links?.open(s.sessionId)}
                    className={ITEM}
                  >
                    <SubagentRow session={s} run={links?.runs.get(s.sessionId)} />
                    <Menu.RadioItemIndicator className="flex size-4 items-center justify-center">
                      <Check aria-hidden className="size-3.5" />
                    </Menu.RadioItemIndicator>
                  </Menu.RadioItem>
                ))}
              </Menu.RadioGroup>
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
    </nav>
  )
}

function SubagentRow({ session, run }: { session: SessionSummary; run: SubagentRun | undefined }) {
  const running = isRunning(run?.status)
  return (
    <>
      {running ? (
        <Spinner
          role="img"
          aria-label="Running"
          className="size-3.5 text-sky-600 dark:text-sky-400"
        />
      ) : run?.status === 'completed' ? (
        <Check
          role="img"
          aria-label="Completed"
          className="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400"
        />
      ) : (
        <CircleDashed aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      )}
      <span className="min-w-0 flex-1 truncate">{session.title}</span>
      <time
        dateTime={new Date(session.updatedAt * 1000).toISOString()}
        className="shrink-0 text-xs text-muted-foreground tabular-nums"
      >
        {relativeTime(session.updatedAt * 1000)}
      </time>
    </>
  )
}
