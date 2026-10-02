import { useEffect, useState } from 'react'
import { Menu } from '@base-ui/react/menu'
import { Check, Copy, Fingerprint } from 'lucide-react'
import { useCopySession, type CopyWhat } from '@/lib/copy-session'

const ITEM =
  'flex items-center gap-2 rounded-md py-1.5 pl-2.5 pr-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground'

/** The header's copy control: the Session's id, or its details for another Session to read. */
export function CopySessionMenu({
  sessionId,
  title,
  workspace,
}: {
  sessionId: string
  title: string
  workspace: string | null
}) {
  const copySession = useCopySession()
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])
  const copy = (what: CopyWhat) => {
    void copySession({ sessionId, title, cwd: workspace }, what)
      .then(() => setCopied(true))
      .catch(console.error)
  }
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label={copied ? 'Copied' : 'Copy session info'}
        title="Copy session info"
        className="app-no-drag grid size-7 place-items-center rounded-md text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-accent data-[popup-open]:text-foreground"
      >
        {copied ? (
          <Check aria-hidden className="size-3.5" />
        ) : (
          <Copy aria-hidden className="size-3.5" />
        )}
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner side="bottom" align="end" sideOffset={6} className="z-50 outline-none">
          <Menu.Popup
            aria-label="Copy session info"
            className="min-w-44 rounded-lg border bg-popover p-1 text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none"
          >
            <Menu.Item onClick={() => copy('id')} className={ITEM}>
              <Fingerprint aria-hidden className="size-4 text-muted-foreground" />
              Copy session ID
            </Menu.Item>
            <Menu.Item onClick={() => copy('details')} className={ITEM}>
              <Copy aria-hidden className="size-4 text-muted-foreground" />
              Copy session details
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
