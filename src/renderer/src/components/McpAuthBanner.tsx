import { KeyRound, ExternalLink, X } from 'lucide-react'
import { useMcpAuthRequired } from '@/store'
import { useAppStore } from '@/store'
import { useCallback } from 'react'

export function McpAuthBanner() {
  const auth = useMcpAuthRequired()

  const dismiss = useCallback(() => {
    const s = useAppStore.getState()
    const sid = s.activeSessionId
    if (!sid) return
    s._setSessionBuffers((prev) => {
      const buf = prev.get(sid)
      if (!buf) return prev
      const next = new Map(prev)
      next.set(sid, { ...buf, mcpAuthRequired: null })
      return next
    })
  }, [])

  if (!auth) return null

  return (
    <div className="mx-auto max-w-3xl px-4">
      <div className="flex items-center gap-3 rounded-lg border border-amber-400/50 bg-amber-500/5 px-4 py-2.5">
        <KeyRound className="size-4 shrink-0 text-amber-500" />
        <div className="flex-1 text-xs">
          <span className="font-medium text-foreground">{auth.serverName}</span>
          <span className="text-muted-foreground"> requires authentication.</span>
        </div>
        <a
          href={auth.authUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-1 rounded-md bg-foreground px-2.5 py-1 text-xs font-medium text-background transition-colors hover:bg-foreground/80"
        >
          Authorize
          <ExternalLink className="size-3" />
        </a>
        <button
          type="button"
          className="rounded p-0.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          onClick={dismiss}
        >
          <X className="size-3.5" />
        </button>
      </div>
    </div>
  )
}
