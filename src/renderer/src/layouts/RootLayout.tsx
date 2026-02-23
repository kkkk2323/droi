import { Outlet } from '@tanstack/react-router'
import { AppSidebar } from '@/components/app-sidebar'
import { SidebarProvider, SidebarInset, SidebarTrigger, useSidebar } from '@/components/ui/sidebar'
import { FilesChangedBadge } from '@/components/FilesChangedBadge'
import { GitActionsButton } from '@/components/GitActionsButton'
import { WorktreeIndicator } from '@/components/WorktreeIndicator'
import { OpenInEditorButton } from '@/components/OpenInEditorButton'
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

function InnerLayout() {
  const activeProjectDir = useActiveProjectDir()
  const isRunning = useIsRunning()
  const activeSessionTitle = useActiveSessionTitle()
  const pendingNewSession = usePendingNewSession()
  const workspaceError = useWorkspaceError()
  const { clearWorkspaceError } = useActions()
  const { open } = useSidebar()
  const isMacElectron =
    !isBrowserMode() &&
    typeof navigator !== 'undefined' &&
    (navigator.userAgent.includes('Macintosh') || navigator.userAgent.includes('Mac OS X'))

  return (
    <>
      <AppSidebar />
      <SidebarInset>
        <header
          className="flex h-10 shrink-0 items-center gap-2 pr-4"
          style={
            {
              WebkitAppRegion: 'drag',
              paddingLeft: isMacElectron ? (open ? '3rem' : '7rem') : open ? '1rem' : '6.5rem',
              transition: 'padding-left 200ms ease-linear',
            } as React.CSSProperties
          }
        >
          {isMacElectron ? (
            <div
              className="fixed top-[4px] left-0 z-10 flex h-10 items-center pl-20"
              style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
            >
              <SidebarTrigger
                className="shrink-0 size-6 p-0"
                style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
              />
            </div>
          ) : (
            <SidebarTrigger
              className="shrink-0 size-6 p-0"
              style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
            />
          )}
          {!pendingNewSession && activeSessionTitle && (
            <div className="pt-2 flex-1 max-w-[200px] truncate text-sm font-medium text-foreground">
              {activeSessionTitle}
            </div>
          )}
          {!pendingNewSession && (
            <div
              className="ml-auto pt-2 flex items-center gap-1.5"
              style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
            >
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
