import { useState } from 'react'
import { FolderGit2, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useNewSession, type RecentWorkspace } from '@/daemon/use-new-session'

export function NewSessionPage({
  recent,
  onCreated,
}: {
  recent: RecentWorkspace[]
  onCreated: (sessionId: string) => void
}) {
  const { create, isCreating, error } = useNewSession()
  const [path, setPath] = useState('')

  const start = async (target: string) => {
    const sessionId = await create(target)
    if (sessionId) onCreated(sessionId)
  }

  return (
    <section
      aria-label="New session"
      className="mx-auto flex h-full w-full max-w-xl flex-col gap-6 overflow-y-auto px-6 py-8"
    >
      <div>
        <h2 className="text-base font-semibold tracking-tight">New session</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Pick a recent workspace or type the path of a directory on the computer running Droi.
        </p>
      </div>

      {recent.length > 0 ? (
        <div>
          <h3 className="mb-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            Recent workspaces
          </h3>
          <ul className="flex flex-col gap-1">
            {recent.map((workspace) => (
              <li key={workspace.path}>
                <button
                  type="button"
                  disabled={isCreating}
                  onClick={() => void start(workspace.path)}
                  className="flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left text-sm hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-50"
                >
                  <FolderGit2 aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate font-medium">{workspace.label}</span>
                    <span className="truncate font-mono text-xs text-muted-foreground">
                      {workspace.path}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <form
        className="flex flex-col gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          void start(path)
        }}
      >
        <label
          htmlFor="new-session-path"
          className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground"
        >
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
            className="h-9 min-w-0 flex-1 rounded-md border bg-background px-3 font-mono text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          />
          <Button type="submit" size="default" disabled={isCreating || !path.trim()}>
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
    </section>
  )
}
