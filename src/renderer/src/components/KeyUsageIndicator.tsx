import { Key } from 'lucide-react'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { useKeysQuery } from '@/hooks/useKeys'

function formatNumber(n: number | null): string {
  if (n === null) return '?'
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, '') + 'B'
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K'
  return Math.round(n).toString()
}

export function KeyUsageIndicator({ className }: { className?: string }) {
  const { data: keys = [] } = useKeysQuery()

  if (keys.length === 0) return null

  const activeKey = keys.find(k => k.isActive)
  const usage = activeKey?.usage
  const percent = usage?.total ? Math.round((usage.used || 0) / usage.total * 100) : null

  const color = percent === null
    ? 'text-muted-foreground'
    : percent >= 80
      ? 'text-red-500'
      : percent >= 60
        ? 'text-yellow-500'
        : 'text-emerald-500'

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger className={`flex items-center justify-center ${className || ''}`}>
          <Key className={`size-3.5 ${color}`} />
        </TooltipTrigger>
        <TooltipContent side="top">
          <div className="space-y-1 text-xs">
            <div>Active: #{activeKey ? activeKey.index + 1 : '?'}</div>
            {usage?.total != null ? (
              <div>
                {formatNumber(usage.used)} / {formatNumber(usage.total)}
                {percent !== null && ` (${percent}%)`}
              </div>
            ) : (
              <div>Usage: N/A</div>
            )}
            {usage?.expires && <div>Expires: {usage.expires}</div>}
            <div>Keys: {keys.length}</div>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
