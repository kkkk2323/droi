import * as React from 'react'
import { useRef, useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { useNavigate } from '@tanstack/react-router'
import {
  useProjects,
  useActiveProjectDir,
  useActiveSessionId,
  useActions,
  useDeletingSessionIds,
  useIsCreatingSession,
  useIsInitialLoadDone,
} from '@/store'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarSeparator,
} from '@/components/ui/sidebar'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import {
  ChevronRightIcon,
  FolderIcon,
  FolderPlusIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlusIcon,
  SettingsIcon,
  Trash2Icon,
  Loader2,
} from 'lucide-react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import { isBrowserMode } from '@/droidClient'
import { useIsMobile } from '@/hooks/use-mobile'
import { getProjectDisplayName } from '@/store/projectHelpers'

function SessionTitle({ title }: { title: string }) {
  const prevRef = useRef(title)
  const [flash, setFlash] = useState(false)

  useEffect(() => {
    if (prevRef.current === title) return
    prevRef.current = title
    setFlash(true)
    const timer = setTimeout(() => setFlash(false), 1200)
    return () => clearTimeout(timer)
  }, [title])

  return (
    <span
      className={cn('block truncate transition-colors duration-500', flash && 'text-foreground')}
    >
      {title}
    </span>
  )
}

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
  const navigate = useNavigate()
  const browserMode = isBrowserMode()
  const mobile = useIsMobile()
  const projects = useProjects()
  const activeProjectDir = useActiveProjectDir()
  const activeSessionId = useActiveSessionId()
  const deletingSessionIds = useDeletingSessionIds()
  const isCreatingSession = useIsCreatingSession()
  const isInitialLoadDone = useIsInitialLoadDone()
  const isInitBlocked = !isInitialLoadDone
  const prevActiveSessionIdRef = useRef(activeSessionId)
  const newSessionRef = useRef<HTMLDivElement | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [renameOpenDir, setRenameOpenDir] = useState<string | null>(null)
  const {
    getSessionRunning,
    handleAddProject,
    handleNewSession,
    handleSelectSession,
    handleDeleteSession,
    handleDeleteProject,
    handleRenameProject,
    handleTogglePin,
  } = useActions()

  useEffect(() => {
    if (activeSessionId !== prevActiveSessionIdRef.current) {
      const wasNew = prevActiveSessionIdRef.current !== activeSessionId
      prevActiveSessionIdRef.current = activeSessionId
      if (wasNew && newSessionRef.current) {
        newSessionRef.current.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
      }
    }
  }, [activeSessionId])

  return (
    <Sidebar variant="sidebar" {...props}>
      <SidebarHeader className="flex-row items-center justify-between py-3 pl-20 pr-2" />

      {!browserMode && (
        <div data-slot="sidebar-new-project" className="px-2 pb-1">
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                tooltip="Add project"
                aria-disabled={isInitBlocked}
                className={cn(isInitBlocked && 'pointer-events-none opacity-60')}
                onClick={() => {
                  if (isInitBlocked) return
                  handleAddProject()
                }}
              >
                <FolderPlusIcon className="size-4" />
                <span className="text-sm font-medium">New Project</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </div>
      )}

      <SidebarContent className="overflow-hidden">
        <ScrollArea className="flex-1">
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {projects.length === 0 && (
                  <SidebarMenuItem>
                    <div className="px-3 py-6 text-center text-xs text-muted-foreground">
                      {browserMode ? (
                        <>
                          No projects available.
                          <br />
                          Add a project on the desktop app first.
                        </>
                      ) : (
                        <>
                          No projects yet.
                          <br />
                          Click <FolderPlusIcon className="inline size-3" /> to add one.
                        </>
                      )}
                    </div>
                  </SidebarMenuItem>
                )}

                {projects.map((project) => {
                  const isActiveProject = project.sessions.some(
                    (s) => s.projectDir === activeProjectDir,
                  )

                  return (
                    <Collapsible
                      key={project.dir}
                      defaultOpen={isActiveProject}
                      render={<SidebarMenuItem className="group/project" />}
                    >
                      <CollapsibleTrigger
                        render={
                          <SidebarMenuButton
                            className="h-9"
                            tooltip={project.dir}
                            onClick={() => {
                              if (!isActiveProject) {
                                const latest = project.sessions[0]
                                if (latest) handleSelectSession(latest.id)
                              }
                            }}
                          />
                        }
                      >
                        <FolderIcon className="size-4" />
                        <span className="truncate">{getProjectDisplayName(project)}</span>
                      </CollapsibleTrigger>

                      <div className="absolute top-1.5 right-1 flex items-center gap-0.5">
                        <button
                          type="button"
                          className={cn(
                            'flex size-5 items-center justify-center rounded-md transition-colors hover:bg-sidebar-accent',
                            mobile ? 'opacity-100' : 'opacity-0 group-hover/project:opacity-100',
                            (isCreatingSession || isInitBlocked) &&
                              'opacity-60 pointer-events-none',
                          )}
                          onClick={(e) => {
                            e.stopPropagation()
                            if (isCreatingSession || isInitBlocked) return
                            handleNewSession(project.dir)
                          }}
                        >
                          {isCreatingSession ? (
                            <Loader2 className="size-3.5 text-muted-foreground animate-spin" />
                          ) : (
                            <PlusIcon className="size-3.5 text-muted-foreground" />
                          )}
                        </button>

                        <DropdownMenu>
                          <DropdownMenuTrigger
                            className={`flex size-5 items-center justify-center rounded-md transition-opacity hover:bg-sidebar-accent ${mobile ? 'opacity-100' : 'opacity-0 group-hover/project:opacity-100'}`}
                            onClick={(e) => e.stopPropagation()}
                            render={<button />}
                          >
                            <MoreHorizontalIcon className="size-3.5 text-muted-foreground" />
                          </DropdownMenuTrigger>
                          {!browserMode && (
                            <DropdownMenuContent side="right" align="start">
                              <AlertDialog
                                open={renameOpenDir === project.dir}
                                onOpenChange={(open) => {
                                  if (!open) setRenameOpenDir(null)
                                }}
                              >
                                <AlertDialogTrigger
                                  render={
                                    <DropdownMenuItem
                                      closeOnClick={false}
                                      className="py-1 cursor-pointer text-xs"
                                    />
                                  }
                                  onClick={() => {
                                    setRenameValue(getProjectDisplayName(project))
                                    setRenameOpenDir(project.dir)
                                  }}
                                >
                                  <PencilIcon className="size-3.5" />
                                  Rename
                                </AlertDialogTrigger>
                                <AlertDialogContent>
                                  <AlertDialogHeader>
                                    <AlertDialogTitle>Rename project</AlertDialogTitle>
                                    <AlertDialogDescription>
                                      Set a custom display name for this project.
                                    </AlertDialogDescription>
                                  </AlertDialogHeader>
                                  <input
                                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                                    value={renameValue}
                                    onChange={(e) => setRenameValue(e.target.value)}
                                    onKeyDown={(e) => {
                                      e.stopPropagation()
                                      if (e.key === 'Enter' && renameValue.trim()) {
                                        handleRenameProject(project.dir, renameValue.trim())
                                        setRenameOpenDir(null)
                                      }
                                      if (e.key === 'Escape') setRenameOpenDir(null)
                                    }}
                                    autoFocus
                                  />
                                  <AlertDialogFooter>
                                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                                    <AlertDialogAction
                                      disabled={!renameValue.trim()}
                                      onClick={() => {
                                        handleRenameProject(project.dir, renameValue.trim())
                                        setRenameOpenDir(null)
                                      }}
                                    >
                                      Save
                                    </AlertDialogAction>
                                  </AlertDialogFooter>
                                </AlertDialogContent>
                              </AlertDialog>
                              <AlertDialog>
                                <AlertDialogTrigger
                                  render={
                                    <DropdownMenuItem
                                      variant="destructive"
                                      closeOnClick={false}
                                      className="py-1 cursor-pointer "
                                    />
                                  }
                                >
                                  <Trash2Icon className="size-3.5" />
                                  Delete
                                </AlertDialogTrigger>
                                <AlertDialogContent>
                                  <AlertDialogHeader>
                                    <AlertDialogTitle>Delete project?</AlertDialogTitle>
                                    <AlertDialogDescription>
                                      This will remove &quot;{getProjectDisplayName(project)}&quot;
                                      from the sidebar. Your files on disk will not be affected.
                                    </AlertDialogDescription>
                                  </AlertDialogHeader>
                                  <AlertDialogFooter>
                                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                                    <AlertDialogAction
                                      variant="destructive"
                                      onClick={() => handleDeleteProject(project.dir)}
                                    >
                                      Delete
                                    </AlertDialogAction>
                                  </AlertDialogFooter>
                                </AlertDialogContent>
                              </AlertDialog>
                            </DropdownMenuContent>
                          )}
                        </DropdownMenu>

                        <CollapsibleTrigger
                          render={
                            <button className="flex size-5 items-center justify-center rounded-md text-sidebar-foreground transition-transform hover:bg-sidebar-accent aria-expanded:rotate-90" />
                          }
                        >
                          <ChevronRightIcon className="size-4" />
                        </CollapsibleTrigger>
                      </div>

                      <CollapsibleContent>
                        {(() => {
                          const pinned = project.sessions.filter((s) => s.pinned)
                          const recent = project.sessions.filter((s) => !s.pinned)
                          return (
                            <SidebarMenuSub>
                              {pinned.length > 0 && (
                                <>
                                  <div className="px-2 pt-2 pb-1 text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider">
                                    Pinned
                                  </div>
                                  <SessionList sessions={pinned} />
                                  {recent.length > 0 && (
                                    <div className="px-2 pt-3 pb-1 text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider">
                                      Recent
                                    </div>
                                  )}
                                </>
                              )}
                              <SessionList sessions={recent} />
                            </SidebarMenuSub>
                          )

                          function SessionList({ sessions }: { sessions: typeof pinned }) {
                            return (
                              <AnimatePresence initial={false}>
                                {sessions.map((session, sessionIdx) => (
                                  <SessionItem
                                    key={session.id}
                                    session={session}
                                    sessionIdx={sessionIdx}
                                  />
                                ))}
                              </AnimatePresence>
                            )
                          }

                          function SessionItem({
                            session,
                            sessionIdx,
                          }: {
                            session: (typeof pinned)[0]
                            sessionIdx: number
                          }) {
                            const isActive = session.id === activeSessionId
                            const isSessionRunning = getSessionRunning(session.id)
                            const isSessionDeleting = deletingSessionIds.has(session.id)
                            const branchName = session.branch?.split('/').pop() || session.branch
                            return (
                              <motion.div
                                key={session.id}
                                ref={isActive ? newSessionRef : undefined}
                                initial={{ opacity: 0, y: -4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: -4 }}
                                transition={{
                                  duration: 0.2,
                                  delay: sessionIdx * 0.03,
                                  ease: [0.16, 1, 0.3, 1],
                                }}
                              >
                                <SidebarMenuSubItem className="group/session">
                                  <SidebarMenuSubButton
                                    render={<button type="button" />}
                                    className={cn(
                                      'w-full max-w-full pr-6 h-auto py-1.5 flex-col items-start gap-0',
                                      isSessionDeleting && 'opacity-60 pointer-events-none',
                                    )}
                                    isActive={isActive}
                                    aria-disabled={isSessionDeleting}
                                    onClick={() => {
                                      if (!isSessionDeleting) handleSelectSession(session.id)
                                    }}
                                  >
                                    <span className="flex w-full items-center gap-1.5">
                                      {isSessionRunning && (
                                        <Loader2 className="size-3 animate-spin text-emerald-500 shrink-0" />
                                      )}
                                      <SessionTitle title={session.title} />
                                    </span>
                                    <span className="flex w-full items-center gap-1.5 text-xs text-muted-foreground">
                                      {branchName && <span className="truncate">{branchName}</span>}
                                      {branchName && <span>·</span>}
                                      <span className="shrink-0">
                                        {formatRelativeTime(
                                          session.lastMessageAt ?? session.savedAt,
                                        )}
                                      </span>
                                    </span>
                                  </SidebarMenuSubButton>
                                  <DropdownMenu>
                                    <DropdownMenuTrigger
                                      className="absolute top-1/2 right-1 opacity-0 -translate-y-1/2 rounded p-0.5 hover:bg-sidebar-accent group-hover/session:opacity-100 data-[popup-open]:opacity-100"
                                      onClick={(e) => e.stopPropagation()}
                                      render={<button type="button" />}
                                    >
                                      <MoreHorizontalIcon className="size-3.5 text-muted-foreground" />
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent side="right" align="start">
                                      <DropdownMenuItem
                                        className="py-1 cursor-pointer text-xs"
                                        onClick={() => handleTogglePin(session.id)}
                                      >
                                        {session.pinned ? 'Unpin' : 'Pin'}
                                      </DropdownMenuItem>
                                      <AlertDialog>
                                        <AlertDialogTrigger
                                          render={
                                            <DropdownMenuItem
                                              variant="destructive"
                                              closeOnClick={false}
                                              className="py-1 cursor-pointer text-xs"
                                            />
                                          }
                                          disabled={isSessionDeleting}
                                        >
                                          {isSessionDeleting ? 'Deleting...' : 'Delete'}
                                        </AlertDialogTrigger>
                                        <AlertDialogContent>
                                          <AlertDialogHeader>
                                            <AlertDialogTitle>Delete session?</AlertDialogTitle>
                                            <AlertDialogDescription>
                                              {session.workspaceType === 'worktree' ? (
                                                <>
                                                  This will permanently delete this session and{' '}
                                                  <span className="font-medium">force delete</span>{' '}
                                                  its worktree.
                                                  <br />
                                                  <span className="font-mono text-[11px]">
                                                    {session.projectDir}
                                                  </span>
                                                  <br />
                                                  Uncommitted changes in this worktree will be lost.
                                                </>
                                              ) : (
                                                'This will permanently delete this session.'
                                              )}
                                            </AlertDialogDescription>
                                          </AlertDialogHeader>
                                          <AlertDialogFooter>
                                            <AlertDialogCancel disabled={isSessionDeleting}>
                                              Cancel
                                            </AlertDialogCancel>
                                            <AlertDialogAction
                                              variant="destructive"
                                              disabled={isSessionDeleting}
                                              onClick={() => {
                                                void handleDeleteSession(session.id)
                                              }}
                                            >
                                              {isSessionDeleting ? (
                                                <>
                                                  <Loader2 className="size-3 animate-spin" />
                                                  Deleting...
                                                </>
                                              ) : (
                                                'Delete'
                                              )}
                                            </AlertDialogAction>
                                          </AlertDialogFooter>
                                        </AlertDialogContent>
                                      </AlertDialog>
                                    </DropdownMenuContent>
                                  </DropdownMenu>
                                </SidebarMenuSubItem>
                              </motion.div>
                            )
                          }
                        })()}
                      </CollapsibleContent>
                    </Collapsible>
                  )
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </ScrollArea>
      </SidebarContent>

      <SidebarSeparator />

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton onClick={() => navigate({ to: '/settings' })}>
              <SettingsIcon className="size-4" />
              <span>Settings</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}

function formatRelativeTime(ts: number): string {
  const diff = Date.now() - ts
  const minutes = Math.floor(diff / 60000)
  if (minutes < 1) return 'now'
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d`
  const months = Math.floor(days / 30)
  return `${months}mo`
}
