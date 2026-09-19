import { Suspense, lazy, useEffect, useMemo, useState } from 'react'
import { Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
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
  useAppStore,
} from '@/store'
import { isBrowserMode } from '@/droidClient'
import { resolveCommitDialogHostState } from '@/lib/commitDialogState'
import { cn } from '@/lib/utils'
import { getAppRouteTarget } from '@/lib/sessionRouting'
import { supportsGitWorkspace } from '@/lib/workspaceType'

const CommitWizard = lazy(() =>
  import('@/components/commit/CommitWizard').then((module) => ({ default: module.CommitWizard })),
)
const UpdateNotification = lazy(() =>
  import('@/components/UpdateNotification').then((module) => ({
    default: module.UpdateNotification,
  })),
)

const noDrag = { WebkitAppRegion: 'no-drag' } as React.CSSProperties

const isMacElectron =
  !isBrowserMode() &&
  typeof navigator !== 'undefined' &&
  (navigator.userAgent.includes('Macintosh') || navigator.userAgent.includes('Mac OS X'))

function InnerLayout() {
  const navigate = useNavigate()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const activeProjectDir = useActiveProjectDir()
  const isRunning = useIsRunning()
  const activeSessionTitle = useActiveSessionTitle()
  const pendingNewSession = usePendingNewSession()
  const workspaceError = useWorkspaceError()
  const { clearWorkspaceError } = useActions()
  const { open } = useSidebar()
  const [commitDialogProjectDir, setCommitDialogProjectDir] = useState<string | null>(null)
  const routeTarget = useAppStore((s) => {
    const activeSessionId = s.activeSessionId
    const activeSession = activeSessionId
      ? s.sessionBuffers.get(activeSessionId) ||
        s.projects
          .flatMap((project) => project.sessions)
          .find((session) => session.id === activeSessionId)
      : null
    return getAppRouteTarget({
      hasPendingNewSession: Boolean(s.pendingNewSession),
      activeSession,
    })
  })
  const activeSessionSupportsGit = useAppStore((s) => {
    const activeSessionId = s.activeSessionId
    const activeSession = activeSessionId
      ? s.sessionBuffers.get(activeSessionId) ||
        s.projects
          .flatMap((project) => project.sessions)
          .find((session) => session.id === activeSessionId)
      : null
    return supportsGitWorkspace(activeSession?.workspaceType)
  })
  const commitDialog = useMemo(
    () =>
      resolveCommitDialogHostState({
        activeProjectDir,
        requestedProjectDir: commitDialogProjectDir,
      }),
    [activeProjectDir, commitDialogProjectDir],
  )

  useEffect(() => {
    if (pathname !== '/' && pathname !== '/mission') return
    if (pathname === routeTarget) return
    navigate({ to: routeTarget, replace: true })
  }, [navigate, pathname, routeTarget])

  return (
    <>
      <AppSidebar />
      <SidebarInset>
        <header
          className={cn(
            'flex h-10 shrink-0 items-center gap-2 pt-2 pr-4 transition-[padding-left] duration-200 ease-linear',
            isMacElectron ? (open ? 'pl-12' : 'pl-28') : open ? 'pl-4' : 'pl-26',
          )}
          style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
        >
          {isMacElectron ? (
            <div className="fixed top-1 left-0 z-10 flex h-10 items-center pl-20" style={noDrag}>
              <SidebarTrigger data-testid="sidebar-trigger" className="shrink-0 size-6 p-0" />
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
              {activeSessionSupportsGit && (
                <>
                  <WorktreeIndicator />
                  <GitActionsButton
                    projectDir={activeProjectDir}
                    isRunning={isRunning}
                    onOpenCommitDialog={(projectDir) => setCommitDialogProjectDir(projectDir)}
                  />
                  <FilesChangedBadge projectDir={activeProjectDir} isRunning={isRunning} />
                </>
              )}
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

      <Suspense fallback={null}>
        <CommitWizard
          open={commitDialog.open}
          onOpenChange={(nextOpen) => {
            if (!nextOpen) setCommitDialogProjectDir(null)
          }}
          projectDir={commitDialog.projectDir}
        />

        <UpdateNotification />
      </Suspense>
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
