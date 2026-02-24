import React, { useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, FileCode, MessageSquare } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
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
    const name = String(
      (raw as any).name || (raw as any).toolName || (raw as any).recipient_name || '',
    )
    const normalized = name.split('.').pop() || name
    if (!/exit\s?spec/i.test(normalized)) continue

    const input =
      (raw as any).input && typeof (raw as any).input === 'object'
        ? (raw as any).input
        : (raw as any).parameters && typeof (raw as any).parameters === 'object'
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

function optionLabel(meta: PermissionOptionMeta): string {
  switch (meta.value) {
    case 'proceed_once':
      return 'Proceed once'
    case 'proceed_always':
      return 'Proceed always'
    case 'proceed_auto_run':
      return 'Auto-run'
    case 'proceed_auto_run_low':
      return 'Auto-run (Low)'
    case 'proceed_auto_run_medium':
      return 'Auto-run (Medium)'
    case 'proceed_auto_run_high':
      return 'Auto-run (High)'
    case 'proceed_edit':
      return 'Proceed edit'
    case 'cancel':
      return meta.label || 'Cancel'
    default:
      return meta.label
  }
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
    <Collapsible
      open={expanded}
      onOpenChange={setExpanded}
      className="my-4 rounded-xl border border-border overflow-hidden"
    >
      <CollapsibleTrigger
        render={<button type="button" />}
        className="flex w-full items-center gap-2.5 px-4 py-3 text-left hover:bg-accent/30 transition-colors"
      >
        <FileCode className="size-4 shrink-0 text-foreground/70" />
        <span className="text-sm font-medium text-foreground">
          {title || 'Implementation Plan'}
        </span>
        <span className="ml-auto text-[11px] text-muted-foreground">Review & approve</span>
        {expanded ? (
          <ChevronDown className="size-3.5 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-3.5 text-muted-foreground" />
        )}
      </CollapsibleTrigger>

      <CollapsibleContent>
        <div className="px-4 pb-3">
          <ScrollArea className="max-h-[50vh] min-h-0 rounded-lg bg-zinc-50 dark:bg-zinc-900/50">
            <div className="px-4 py-3 prose prose-sm max-w-none text-foreground/90 prose-headings:text-foreground prose-p:leading-relaxed prose-pre:bg-zinc-950 prose-pre:text-zinc-200 prose-pre:overflow-x-auto prose-code:text-foreground prose-code:break-all overflow-hidden break-words">
              <Streamdown>{plan}</Streamdown>
            </div>
          </ScrollArea>
        </div>

        <div className="border-t border-border px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            {proceedOptions.map((opt, i) => (
              <Button
                key={opt.value}
                size="sm"
                variant={i === 0 ? 'default' : 'outline'}
                className="text-xs"
                onClick={() => handleProceed(opt)}
              >
                {optionLabel(opt)}
              </Button>
            ))}
            <div className="flex-1" />
            <Button
              variant="ghost"
              size="sm"
              className="text-xs text-muted-foreground"
              onClick={handleCancel}
            >
              <MessageSquare className="size-3" />
              {cancelOption ? optionLabel(cancelOption) : 'Request changes'}
            </Button>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

export function isExitSpecPermission(request?: PendingPermissionRequest | null): boolean {
  if (!request || !Array.isArray(request.toolUses)) return false
  return request.toolUses.some((item) => {
    const raw = (item as any)?.toolUse || item
    if (!raw || typeof raw !== 'object') return false
    const name = String(
      (raw as any).name || (raw as any).toolName || (raw as any).recipient_name || '',
    )
    const normalized = name.split('.').pop() || name
    return /exit\s?spec/i.test(normalized)
  })
}
