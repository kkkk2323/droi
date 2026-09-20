import type { ReactNode } from 'react'

/**
 * Top strip of every page in the main panel: doubles as the window drag
 * region and carries the page title. Controls stay in the sidebar and the
 * composer so the header reads as chrome, not as a toolbar.
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
    <header className="app-drag flex h-[calc(env(safe-area-inset-top)+2.75rem)] shrink-0 items-center gap-1 px-2 pt-[env(safe-area-inset-top)] md:px-3">
      {leading}
      <div className="flex min-w-0 flex-1 items-center gap-1 text-sm font-medium">{title}</div>
      {children ? <div className="flex shrink-0 items-center gap-0.5">{children}</div> : null}
    </header>
  )
}
