import React, { useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, FileCode, MessageSquare } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { ScrollArea } from '@/components/ui/scroll-area'
import type { PendingPermissionRequest, PermissionOptionMeta } from '@/state/appReducer'
import type { DroidPermissionOption } from '@/types'

interface ExitSpecData {
  plan: string
  title?: string
}

function extractExitSpecData(request: PendingPermissionRequest): ExitSpecData | null {
  if (!Array.isArray(request.toolUses)) return null
  for (const item of request.toolUses) {
    const raw = (item as any)?.toolUse || item
    if (!raw || typeof raw !== 'object') continue
    const name = String((raw as any).name || (raw as any).toolName || (raw as any).recipient_name || '')
    const normalized = name.split('.').pop() || name
    if (!/exit\s?spec/i.test(normalized)) continue

    const input = ((raw as any).input && typeof (raw as any).input === 'object')
      ? (raw as any).input
      : ((raw as any).parameters && typeof (raw as any).parameters === 'object')
        ? (raw as any).parameters
        : null
    if (!input || typeof input.plan !== 'string') continue
    return {
      plan: input.plan,
      title: typeof input.title === 'string' ? input.title : undefined,
    }
  }
  return null
}

interface SpecReviewCardProps {
  request: PendingPermissionRequest
  onRespond: (params: { selectedOption: DroidPermissionOption }) => void
  onRequestChanges: () => void
}

export function SpecReviewCard({ request, onRespond, onRequestChanges }: SpecReviewCardProps) {
  const [expanded, setExpanded] = useState(true)

  useEffect(() => {
    setExpanded(true)
  }, [request.requestId])

  const data = extractExitSpecData(request)
  if (!data) return null

  const { plan, title } = data

  const proceedOptions = request.optionsMeta.filter((o) => o.value !== 'cancel')
  const cancelOption = request.optionsMeta.find((o) => o.value === 'cancel')

  const handleProceed = (meta: PermissionOptionMeta) => {
    onRespond({ selectedOption: meta.value })
  }

  const handleCancel = () => {
    if (cancelOption) onRespond({ selectedOption: cancelOption.value })
    onRequestChanges()
  }

  return (
    <Collapsible open={expanded} onOpenChange={setExpanded} className="my-4 rounded-xl border border-border overflow-hidden">
      <CollapsibleTrigger
        render={<button type="button" />}
        className="flex w-full items-center gap-2.5 px-4 py-3 text-left hover:bg-accent/30 transition-colors"
      >
        <FileCode className="size-4 shrink-0 text-blue-500" />
        <span className="text-sm font-medium text-foreground">
          {title || 'Implementation Plan'}
        </span>
        <span className="ml-auto text-[11px] text-muted-foreground">Review & approve</span>
        {expanded
          ? <ChevronDown className="size-3.5 text-muted-foreground" />
          : <ChevronRight className="size-3.5 text-muted-foreground" />}
      </CollapsibleTrigger>

      <CollapsibleContent>
        <div className="px-4 pb-3">
          <ScrollArea className="h-[50vh] rounded-lg bg-zinc-50 dark:bg-zinc-900/50">
            <div className="px-4 py-3 prose prose-sm max-w-none text-foreground/90 prose-headings:text-foreground prose-p:leading-relaxed prose-pre:bg-zinc-950 prose-pre:text-zinc-200 prose-pre:overflow-x-auto prose-code:text-foreground prose-code:break-all overflow-hidden break-words">
              <Streamdown>{plan}</Streamdown>
            </div>
          </ScrollArea>
        </div>

        <div className="border-t border-border px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <ProceedButtonGroup options={proceedOptions} onSelect={handleProceed} />
            <div className="flex-1" />
            <Button variant="ghost" size="sm" className="text-xs text-muted-foreground" onClick={handleCancel}>
              <MessageSquare className="size-3" />
              {cancelOption?.label || 'Request changes'}
            </Button>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

function ProceedButtonGroup({ options, onSelect }: {
  options: PermissionOptionMeta[]
  onSelect: (meta: PermissionOptionMeta) => void
}) {
  if (options.length === 0) return null

  const primary = options[0]
  const autoRunOptions = options.filter((o) =>
    o.value === 'proceed_auto_run_low'
    || o.value === 'proceed_auto_run_medium'
    || o.value === 'proceed_auto_run_high'
  )
  const hasAutoRun = autoRunOptions.length > 0

  const autoRunLevel = (value: string): string => {
    if (value === 'proceed_auto_run_low') return 'Low'
    if (value === 'proceed_auto_run_medium') return 'Medium'
    if (value === 'proceed_auto_run_high') return 'High'
    return value
  }

  return (
    <div className="flex items-center" data-slot="button-group">
      <Button
        size="sm"
        className={cn('text-xs', hasAutoRun && 'rounded-r-none')}
        onClick={() => onSelect(primary)}
      >
        {primary.label}
      </Button>
      {hasAutoRun && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button size="sm" className="rounded-l-none border-l border-background/20 px-2" />
            }
          >
            <ChevronDown className="size-3" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" align="start" sideOffset={4} className="w-[260px]">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Auto-run permission level</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {autoRunOptions.map((opt) => (
                <DropdownMenuItem key={opt.value} onClick={() => onSelect(opt)}>
                  <div className="flex flex-col gap-0.5">
                    <span className="font-semibold text-xs">{autoRunLevel(opt.value)}</span>
                    <span className="text-muted-foreground text-[11px] leading-tight">{opt.label}</span>
                  </div>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}

export function isExitSpecPermission(request?: PendingPermissionRequest | null): boolean {
  if (!request || !Array.isArray(request.toolUses)) return false
  return request.toolUses.some((item) => {
    const raw = (item as any)?.toolUse || item
    if (!raw || typeof raw !== 'object') return false
    const name = String((raw as any).name || (raw as any).toolName || (raw as any).recipient_name || '')
    const normalized = name.split('.').pop() || name
    return /exit\s?spec/i.test(normalized)
  })
}
