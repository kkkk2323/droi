import { useState, type ReactNode } from 'react'
import { Menu } from '@base-ui/react/menu'
import { Check, ChevronDown, Folder, FolderPlus, Loader2 } from 'lucide-react'
import { DroiMark } from '@/components/droi-mark'
import { InputBar, type Submission } from '@/components/chat/input-bar'
import { COLUMN } from '@/components/chat/session-view'
import { Button } from '@/components/ui/button'
import { useDraftSession } from '@/daemon/use-draft-session'
import { useNewSession, type RecentWorkspace } from '@/daemon/use-new-session'
import { useSessionDefaults } from '@/daemon/use-session-defaults'
import { useSlashItems, type SlashItem } from '@/daemon/use-slash-items'
import { SettingsControls } from '@/components/chat/session-toolbar'
import { setPendingPrompt } from '@/lib/pending-prompt'
import { cn } from '@/lib/utils'

const NO_BUILTINS: SlashItem[] = []

/**
 * Waku-style start page: one question with the Workspace as a menu inside
 * it, and the composer underneath. The first message rides along into the
 * Session; sending nothing just opens it.
 */
export function NewSessionPage({
  recent,
  initialWorkspace = null,
  onCreated,
  header,
}: {
  recent: RecentWorkspace[]
  /** Preselected Workspace (the page was opened from a sidebar group). */
  initialWorkspace?: string | null
  onCreated: (sessionId: string) => void
  header: ReactNode
}) {
  const { create, isCreating, error } = useNewSession()
  // Until the user picks, the most recent Workspace is the target; recents
  // arrive with the session list, so the default is derived, not stored.
  const [choice, setChoice] = useState<{ kind: 'recent'; path: string } | { kind: 'other' } | null>(
    initialWorkspace ? { kind: 'recent', path: initialWorkspace } : null,
  )
  const workspace =
    choice === null ? (recent[0]?.path ?? null) : choice.kind === 'recent' ? choice.path : null
  const typing = choice?.kind === 'other' || (choice === null && recent.length === 0)
  const setWorkspace = (path: string) => setChoice({ kind: 'recent', path })
  const setOther = () => setChoice({ kind: 'other' })
  const [path, setPath] = useState('')

  // The Daemon's defaults, with whatever the user changed on this page on top.
  const defaults = useSessionDefaults()
  const [overrides, setOverrides] = useState<{
    modelId?: string
    reasoningEffort?: string
    autonomyLevel?: string
  }>({})
  const settings = {
    models: defaults.models,
    modelId: overrides.modelId ?? defaults.modelId,
    reasoningEffort: overrides.reasoningEffort ?? defaults.reasoningEffort,
    autonomyLevel: overrides.autonomyLevel ?? defaults.autonomyLevel,
  }
  const pickModel = (modelId: string) => {
    // A new model may not offer the current effort; fall back to its list.
    const model = defaults.models.find((m) => m.id === modelId)
    const effort = settings.reasoningEffort
    const keep = effort && model?.reasoningEfforts.includes(effort)
    setOverrides({
      ...overrides,
      modelId,
      ...(keep ? {} : { reasoningEffort: model?.reasoningEfforts[0] ?? undefined }),
    })
  }

  const draft = useDraftSession(workspace)
  // `/compact` has nothing to summarise yet, so only the Workspace's own items.
  const slashItems = useSlashItems(draft.sessionId, NO_BUILTINS)

  const start = async (target: string, prompt?: Submission) => {
    const sessionId = (await draft.take(target, settings)) ?? (await create(target, settings))
    if (!sessionId) return
    if (prompt && (prompt.text.trim() || prompt.images.length > 0)) {
      setPendingPrompt(sessionId, { text: prompt.text, images: prompt.images })
    }
    onCreated(sessionId)
  }

  const label = workspace
    ? (recent.find((w) => w.path === workspace)?.label ?? name(workspace))
    : null

  return (
    <section aria-label="New session" className="flex h-full min-h-0 flex-col">
      {header}
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-5 overflow-y-auto px-6 text-center">
        <DroiMark className="size-9" />
        <h2 className="flex flex-wrap items-baseline justify-center gap-x-1.5 text-xl font-medium tracking-tight">
          {label ? (
            <>
              <span>What do you want to build in</span>
              <span className="inline-flex items-baseline">
                <WorkspaceMenu
                  recent={recent}
                  value={workspace}
                  onPick={setWorkspace}
                  onOther={setOther}
                >
                  {label}
                </WorkspaceMenu>
                ?
              </span>
            </>
          ) : (
            <>
              <span>Where should Droid work?</span>
              {recent.length > 0 ? (
                <WorkspaceMenu
                  recent={recent}
                  value={workspace}
                  onPick={setWorkspace}
                  onOther={setOther}
                >
                  Choose a workspace
                </WorkspaceMenu>
              ) : null}
            </>
          )}
        </h2>

        {typing ? (
          <form
            className="flex w-full max-w-md gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              void start(path)
            }}
          >
            <input
              aria-label="Workspace path"
              value={path}
              onChange={(event) => setPath(event.target.value)}
              placeholder="/Users/you/projects/app"
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              autoFocus={recent.length > 0}
              className="h-9 min-w-0 flex-1 rounded-lg border bg-background px-3 font-mono text-sm outline-none transition-colors focus-visible:border-ring/60 focus-visible:ring-2 focus-visible:ring-ring/30"
            />
            <Button type="submit" disabled={isCreating || !path.trim()}>
              {isCreating ? <Loader2 aria-hidden className="animate-spin" /> : null}
              Start
            </Button>
          </form>
        ) : null}
        {error ? (
          <p role="alert" className="text-sm text-destructive-foreground">
            {error}
          </p>
        ) : null}
      </div>

      <div className={cn(COLUMN, 'shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]')}>
        <InputBar
          isRunning={false}
          disabled={!workspace || isCreating || draft.isTaking}
          allowEmpty
          placeholder="Do anything…"
          sendLabel="Start session"
          onSend={(submission) => {
            if (workspace) void start(workspace, submission)
          }}
          onCancel={() => {}}
          error={null}
          slashItems={slashItems}
          footer={
            <SettingsControls
              settings={settings}
              onModel={pickModel}
              onReasoningEffort={(reasoningEffort) =>
                setOverrides({ ...overrides, reasoningEffort })
              }
              onAutonomyLevel={(autonomyLevel) => setOverrides({ ...overrides, autonomyLevel })}
            />
          }
        />
        <div className="flex h-7 items-center gap-3 px-2 text-xs text-muted-foreground">
          {workspace ? (
            <span className="flex min-w-0 items-center gap-1.5" title={workspace}>
              <Folder aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{workspace}</span>
            </span>
          ) : null}
        </div>
      </div>
    </section>
  )
}

function WorkspaceMenu({
  recent,
  value,
  onPick,
  onOther,
  children,
}: {
  recent: RecentWorkspace[]
  value: string | null
  onPick: (path: string) => void
  onOther: () => void
  children: ReactNode
}) {
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label="Workspace"
        className="inline-flex items-baseline gap-0.5 border-b border-dashed border-muted-foreground/50 outline-none transition-colors hover:border-foreground focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:border-foreground"
      >
        {children}
        <ChevronDown aria-hidden className="size-3.5 self-center opacity-50" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner side="bottom" align="start" sideOffset={6} className="z-50 outline-none">
          <Menu.Popup className="max-h-[min(22rem,var(--available-height))] min-w-48 overflow-y-auto rounded-lg border bg-popover p-1 text-left text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none">
            <Menu.RadioGroup value={value} onValueChange={(next) => onPick(String(next))}>
              {recent.map((workspace) => (
                <Menu.RadioItem
                  key={workspace.path}
                  value={workspace.path}
                  title={workspace.path}
                  closeOnClick
                  className="flex items-center gap-3 rounded-md py-1.5 pl-2.5 pr-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
                >
                  <span className="min-w-0 flex-1 truncate">{workspace.label}</span>
                  <Menu.RadioItemIndicator className="flex size-4 items-center justify-center">
                    <Check aria-hidden className="size-3.5" />
                  </Menu.RadioItemIndicator>
                </Menu.RadioItem>
              ))}
            </Menu.RadioGroup>
            <Menu.Separator className="my-1 h-px bg-border" />
            <Menu.Item
              onClick={onOther}
              className="flex items-center gap-2 rounded-md py-1.5 pl-2.5 pr-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
            >
              <FolderPlus aria-hidden className="size-4 text-muted-foreground" />
              Other folder…
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}

function name(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}
