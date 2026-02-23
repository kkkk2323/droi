import { Plug, Loader2, AlertCircle, ExternalLink } from 'lucide-react'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { useMcpServers, useMcpAuthRequired } from '@/store'

interface McpServer {
  name: string
  status: string
}

export function McpStatusIndicator({ className }: { className?: string }) {
  const servers = useMcpServers()
  const auth = useMcpAuthRequired()
  if (!servers || !Array.isArray(servers) || servers.length === 0) return null

  const typed = servers as McpServer[]
  const connected = typed.filter((s) => s.status === 'connected')
  const connecting = typed.filter((s) => s.status === 'connecting')
  const errored = typed.filter((s) => s.status === 'error' || s.status === 'failed')

  const color = errored.length > 0
    ? 'text-red-500'
    : connecting.length > 0
      ? 'text-yellow-500'
      : 'text-emerald-500'

  const Icon = connecting.length > 0 && connected.length === 0
    ? Loader2
    : errored.length > 0 && connected.length === 0
      ? AlertCircle
      : Plug

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger className={`flex items-center justify-center ${className || ''}`}>
          <Icon className={`size-3.5 ${color} ${connecting.length > 0 && connected.length === 0 ? 'animate-spin' : ''}`} />
        </TooltipTrigger>
        <TooltipContent side="top">
          <div className="space-y-1.5 text-xs">
            <div>MCP Servers ({typed.length})</div>
            {typed.map((s) => {
              const isFailed = s.status === 'error' || s.status === 'failed'
              const hasAuth = isFailed && auth && auth.serverName === s.name && auth.authUrl
              return (
                <div key={s.name} className="flex items-center gap-2">
                  <span className={`size-1.5 shrink-0 rounded-full ${
                    s.status === 'connected' ? 'bg-emerald-500'
                      : s.status === 'connecting' ? 'bg-yellow-500'
                        : 'bg-red-500'
                  }`} />
                  <span>{s.name}</span>
                  <span className="text-muted-foreground">{s.status}</span>
                  {hasAuth && (
                    <a
                      href={auth.authUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-0.5 text-foreground hover:text-foreground/70 hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      Authorize
                      <ExternalLink className="size-2.5" />
                    </a>
                  )}
                </div>
              )
            })}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
