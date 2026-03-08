import { Outlet } from '@tanstack/react-router'
import { AppSidebar } from '@/components/app-sidebar'
import { SidebarProvider, SidebarInset, SidebarTrigger, useSidebar } from '@/components/ui/sidebar'
import { FilesChangedBadge } from '@/components/FilesChangedBadge'
import { GitActionsButton } from '@/components/GitActionsButton'
import { WorktreeIndicator } from '@/components/WorktreeIndicator'
import { OpenInEditorButton } from '@/components/OpenInEditorButton'
import { UpdateNotification } from '@/components/UpdateNotification'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  useActiveProjectDir,
  useIsRunning,
  useActiveSessionTitle,
  useWorkspaceError,
  useActions,
  usePendingNewSession,
} from '@/store'
import { isBrowserMode } from '@/droidClient'
import { cn } from '@/lib/utils'

const noDrag = { WebkitAppRegion: 'no-drag' } as React.CSSProperties

const isMacElectron =
  !isBrowserMode() &&
  typeof navigator !== 'undefined' &&
  (navigator.userAgent.includes('Macintosh') || navigator.userAgent.includes('Mac OS X'))

function InnerLayout() {
  const activeProjectDir = useActiveProjectDir()
  const isRunning = useIsRunning()
  const activeSessionTitle = useActiveSessionTitle()
  const pendingNewSession = usePendingNewSession()
  const workspaceError = useWorkspaceError()
  const { clearWorkspaceError } = useActions()
  const { open } = useSidebar()

  return (
    <>
      <AppSidebar />
      <SidebarInset>
        <header
          className={cn(
            'flex h-10 shrink-0 items-center gap-2 pt-2 pr-4 transition-[padding-left] duration-200 ease-linear',
            isMacElectron
              ? open ? 'pl-12' : 'pl-28'
              : open ? 'pl-4' : 'pl-26',
          )}
          style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
        >
          {isMacElectron ? (
            <div
              className="fixed top-1 left-0 z-10 flex h-10 items-center pl-20"
              style={noDrag}
            >
              <SidebarTrigger
                data-testid="sidebar-trigger"
                className="shrink-0 size-6 p-0"
              />
            </div>
          ) : (
            <SidebarTrigger
              data-testid="sidebar-trigger"
              className="shrink-0 size-6 p-0"
              style={noDrag}
            />
          )}
          {!pendingNewSession && activeSessionTitle && (
            <div className="flex-1 max-w-48 truncate text-sm font-medium text-foreground">
              {activeSessionTitle}
            </div>
          )}
          {!pendingNewSession && (
            <div className="ml-auto flex items-center gap-1.5" style={noDrag}>
              {activeProjectDir && <OpenInEditorButton dir={activeProjectDir} />}
              <WorktreeIndicator />
              <GitActionsButton projectDir={activeProjectDir} isRunning={isRunning} />
              <FilesChangedBadge projectDir={activeProjectDir} isRunning={isRunning} />
            </div>
          )}
        </header>
        <Outlet />
      </SidebarInset>

      <AlertDialog
        open={Boolean(workspaceError)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) clearWorkspaceError()
        }}
      >
        <AlertDialogContent size="sm">
          <AlertDialogHeader>
            <AlertDialogTitle>Cannot Switch Workspace</AlertDialogTitle>
            <AlertDialogDescription>{workspaceError}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogAction onClick={clearWorkspaceError}>OK</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <UpdateNotification />
    </>
  )
}

export function RootLayout() {
  return (
    <SidebarProvider>
      <InnerLayout />
    </SidebarProvider>
  )
}
