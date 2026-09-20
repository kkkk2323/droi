import { useState, type ReactNode } from 'react'
import { ArrowRight, Folder, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useNewSession, type RecentWorkspace } from '@/daemon/use-new-session'

export function NewSessionPage({
  recent,
  onCreated,
  header,
}: {
  recent: RecentWorkspace[]
  onCreated: (sessionId: string) => void
  header: ReactNode
}) {
  const { create, isCreating, error } = useNewSession()
  const [path, setPath] = useState('')

  const start = async (target: string) => {
    const sessionId = await create(target)
    if (sessionId) onCreated(sessionId)
  }

  return (
    <section aria-label="New session" className="flex h-full min-h-0 flex-col">
      {header}
      <div className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-8 overflow-y-auto px-6 pb-12 pt-6 md:px-8">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Where should Droid work?</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Pick a recent workspace or type the path of a directory on the computer running Droi.
          </p>
        </div>

        <form
          className="flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            void start(path)
          }}
        >
          <label htmlFor="new-session-path" className="text-sm font-medium">
            Workspace path
          </label>
          <div className="flex gap-2">
            <input
              id="new-session-path"
              value={path}
              onChange={(event) => setPath(event.target.value)}
              placeholder="/Users/you/projects/app"
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              className="h-9 min-w-0 flex-1 rounded-lg border bg-background px-3 font-mono text-sm outline-none transition-colors focus-visible:border-ring/60 focus-visible:ring-2 focus-visible:ring-ring/30"
            />
            <Button type="submit" disabled={isCreating || !path.trim()}>
              {isCreating ? <Loader2 aria-hidden className="animate-spin" /> : null}
              Start
            </Button>
          </div>
          {error ? (
            <p role="alert" className="text-sm text-destructive-foreground">
              {error}
            </p>
          ) : null}
        </form>

        {recent.length > 0 ? (
          <div>
            <h3 className="mb-2 text-sm font-medium">Recent workspaces</h3>
            <ul className="flex flex-col gap-1.5">
              {recent.map((workspace) => (
                <li key={workspace.path}>
                  <button
                    type="button"
                    disabled={isCreating}
                    onClick={() => void start(workspace.path)}
                    className="group flex w-full items-center gap-3 rounded-xl bg-card px-4 py-3 text-left text-sm transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-50"
                  >
                    <Folder aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate font-medium">{workspace.label}</span>
                      <span className="truncate font-mono text-xs text-muted-foreground">
                        {workspace.path}
                      </span>
                    </span>
                    <ArrowRight
                      aria-hidden
                      className="size-4 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
                    />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </section>
  )
}
