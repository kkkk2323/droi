import { PanelLeft } from 'lucide-react'
import { Button } from '@/components/ui/button'

/** Shows or hides the sessions sidebar; sits top-left wherever the sidebar edge is. */
export function SidebarToggle({ expanded, onClick }: { expanded: boolean; onClick: () => void }) {
  return (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label={expanded ? 'Hide sidebar' : 'Show sidebar'}
      aria-expanded={expanded}
      aria-controls="sessions-sidebar"
      className="text-muted-foreground"
      onClick={onClick}
    >
      <PanelLeft aria-hidden />
    </Button>
  )
}
