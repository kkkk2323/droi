import type { ReactNode } from 'react'
import { Moon, Sun } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useTheme } from '@/lib/theme'
import { ConnectionStatus } from './connection-status'

/**
 * Top strip of every page in the main panel: doubles as the window drag
 * region, carries the page title and the always-present status controls.
 */
export function PageHeader({
  leading,
  title,
  children,
}: {
  leading?: ReactNode
  title: ReactNode
  children?: ReactNode
}) {
  return (
    <header className="app-drag flex h-11 shrink-0 items-center gap-1 px-2 pt-[env(safe-area-inset-top)] md:px-3">
      {leading}
      <div className="flex min-w-0 flex-1 items-center gap-1 text-sm font-medium">{title}</div>
      <div className="flex shrink-0 items-center gap-0.5">
        {children}
        <ThemeToggle />
        <ConnectionStatus />
      </div>
    </header>
  )
}

function ThemeToggle() {
  const [theme, setTheme] = useTheme()
  const next = theme === 'dark' ? 'light' : 'dark'
  return (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label={`Switch to ${next} theme`}
      onClick={() => setTheme(next)}
    >
      {theme === 'dark' ? <Sun aria-hidden /> : <Moon aria-hidden />}
    </Button>
  )
}
