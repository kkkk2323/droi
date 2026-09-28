// The Session's skills and MCP Servers, behind a wrench in the header. Both
// are the Daemon's: it lists them for the Session and writes the switches to
// the same files the droid CLI and the Factory App use, so a change here shows
// up there and in the next Session too.
import { useState } from 'react'
import { Dialog } from '@base-ui/react/dialog'
import { Wrench, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { McpTab } from './mcp-tab'
import { SkillsTab } from './skills-tab'

type Tab = 'skills' | 'mcp'

const TABS: Array<{ id: Tab; label: string }> = [
  { id: 'skills', label: 'Skills' },
  { id: 'mcp', label: 'MCP servers' },
]

export function ToolsButton({ sessionId }: { sessionId: string }) {
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<Tab>('skills')
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger
        aria-label="Skills and MCP servers"
        className="app-no-drag flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-accent data-[popup-open]:text-foreground"
      >
        <Wrench aria-hidden className="size-3.5" />
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-black/30 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 flex max-h-[min(40rem,85vh)] w-[min(40rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-xl outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none">
          <div className="flex items-center gap-1 border-b px-3 py-2">
            <Dialog.Title className="sr-only">Skills and MCP servers</Dialog.Title>
            <div role="tablist" aria-label="Skills and MCP servers" className="flex gap-0.5">
              {TABS.map(({ id, label }) => (
                <button
                  key={id}
                  type="button"
                  role="tab"
                  aria-selected={tab === id}
                  onClick={() => setTab(id)}
                  className={cn(
                    'h-7 rounded-md px-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
                    tab === id && 'bg-accent text-foreground',
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
            <div className="flex-1" />
            <Dialog.Close
              render={<Button size="icon-xs" variant="ghost" aria-label="Close" />}
              className="text-muted-foreground"
            >
              <X aria-hidden />
            </Dialog.Close>
          </div>
          <div role="tabpanel" className="min-h-0 flex-1 overflow-y-auto p-3">
            {open ? (
              tab === 'skills' ? (
                <SkillsTab sessionId={sessionId} />
              ) : (
                <McpTab sessionId={sessionId} />
              )
            ) : null}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
