import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { useTokenUsage } from '@/store'

function formatTokenCount(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K'
  return String(n)
}

export function TokenUsageIndicator({ className }: { className?: string }) {
  const usage = useTokenUsage()
  if (!usage) return null

  const total = usage.inputTokens + usage.outputTokens
  if (total === 0) return null

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger
          className={`flex items-center gap-1 text-[11px] tabular-nums text-muted-foreground ${className || ''}`}
        >
          <span>{formatTokenCount(total)} tokens</span>
        </TooltipTrigger>
        <TooltipContent side="top">
          <div className="space-y-1 text-xs">
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Input</span>
              <span>{formatTokenCount(usage.inputTokens)}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Output</span>
              <span>{formatTokenCount(usage.outputTokens)}</span>
            </div>
            {usage.cacheReadTokens > 0 && (
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Cache read</span>
                <span>{formatTokenCount(usage.cacheReadTokens)}</span>
              </div>
            )}
            {usage.cacheCreationTokens > 0 && (
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Cache write</span>
                <span>{formatTokenCount(usage.cacheCreationTokens)}</span>
              </div>
            )}
            {usage.thinkingTokens > 0 && (
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Thinking</span>
                <span>{formatTokenCount(usage.thinkingTokens)}</span>
              </div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
