import * as React from 'react'
import { useRef, useState, useEffect } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useProjects, useActiveProjectDir, useActiveSessionId, useActions, useDeletingSessionIds, useIsCreatingSession } from '@/store'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
  PlusIcon,
  SettingsIcon,
  Trash2Icon,
  Loader2,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { isBrowserMode } from '@/droidClient'
import { useIsMobile } from '@/hooks/use-mobile'

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
    <span className={cn(
      'block max-w-[140px] truncate transition-colors duration-500',
      flash && 'text-blue-500',
    )}>
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
  const {
    getSessionRunning,
    handleAddProject,
    handleNewSession,
    handleSelectSession,
    handleDeleteSession,
    handleDeleteProject,
  } = useActions()

  return (
    <Sidebar variant="sidebar" {...props}>
      <SidebarHeader className="flex-row items-center justify-between py-3 pl-20 pr-2" />


      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {!browserMode && (
                <SidebarMenuItem className='pt-2'>
                  <SidebarMenuButton
                    tooltip="Add project"
                    onClick={handleAddProject}
                  >
                    <FolderPlusIcon className="size-4" />
                    <span className="text-sm font-medium">New Project</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )}
              {projects.length === 0 && (
                <SidebarMenuItem>
                  <div className="px-3 py-6 text-center text-xs text-muted-foreground">
                    {browserMode ? (
                      <>
                        No projects available.<br />
                        Add a project on the desktop app first.
                      </>
                    ) : (
                      <>
                        No projects yet.<br />
                        Click <FolderPlusIcon className="inline size-3" /> to add one.
                      </>
                    )}
                  </div>
                </SidebarMenuItem>
              )}

              {projects.map((project) => {
                const isActiveProject = project.sessions.some((s) => s.projectDir === activeProjectDir)

                return (
                  <Collapsible
                    key={`${project.dir}-${isActiveProject}`}
                    defaultOpen={isActiveProject}
                    render={<SidebarMenuItem className="group/project" />}
                  >
                    <CollapsibleTrigger
                      render={
                        <SidebarMenuButton
                          tooltip={project.dir}
                          onClick={() => {
                            if (!isActiveProject) {
                              const latest = project.sessions[0]
                              if (latest) handleSelectSession(latest.id)
                              else if (!isCreatingSession) handleNewSession(project.dir)
                            }
                          }}
                        />
                      }
                    >
                      <FolderIcon className="size-4" />
                      <span className="truncate">{project.name}</span>
                    </CollapsibleTrigger>

                    <div className="absolute top-1.5 right-1 flex items-center gap-0.5">
                      <button
                        type="button"
                        className={cn(
                          'flex size-5 items-center justify-center rounded-md transition-colors hover:bg-sidebar-accent',
                          mobile ? 'opacity-100' : 'opacity-0 group-hover/project:opacity-100',
                          isCreatingSession && 'opacity-60 pointer-events-none'
                        )}
                        onClick={(e) => {
                          e.stopPropagation()
                          if (isCreatingSession) return
                          handleNewSession(project.dir)
                        }}
                      >
                        {isCreatingSession
                          ? <Loader2 className="size-3.5 text-muted-foreground animate-spin" />
                          : <PlusIcon className="size-3.5 text-muted-foreground" />}
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
                                    This will remove &quot;{project.name}&quot; from the sidebar. Your files on disk will not be affected.
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
                      <SidebarMenuSub>
                        {project.sessions.map((session) => {
                          const isActive = session.id === activeSessionId
                          const isSessionRunning = getSessionRunning(session.id)
                          const isSessionDeleting = deletingSessionIds.has(session.id)
                          return (
                          <SidebarMenuSubItem key={session.id} className="group/session">
                            <SidebarMenuSubButton
                              render={<button type="button" />}
                              className={cn('max-w-full pr-8', isSessionDeleting && 'opacity-60 pointer-events-none')}
                              isActive={isActive}
                              aria-disabled={isSessionDeleting}
                              onClick={() => {
                                if (isSessionDeleting) return
                                handleSelectSession(session.id)
                              }}
                            >
                              {isSessionRunning && <Loader2 className="size-3 animate-spin text-blue-500" />}
                              <SessionTitle title={session.title} />
                              <span className="shrink-0 text-[10px] text-muted-foreground group-hover/session:hidden">
                                {formatRelativeTime(session.savedAt)}
                              </span>
                            </SidebarMenuSubButton>
                            <AlertDialog>
                              <AlertDialogTrigger
                                className="absolute top-1/2 right-1 hidden -translate-y-1/2 rounded p-0.5 hover:bg-destructive/10 group-hover/session:block"
                                onClick={(e) => e.stopPropagation()}
                                disabled={isSessionDeleting}
                                render={<button type="button" />}
                              >
                                {isSessionDeleting
                                  ? <Loader2 className="size-3 animate-spin text-muted-foreground" />
                                  : <Trash2Icon className="size-3 text-muted-foreground hover:text-destructive" />}
                              </AlertDialogTrigger>
                              <AlertDialogContent>
                                <AlertDialogHeader>
                                  <AlertDialogTitle>Delete session?</AlertDialogTitle>
                                  <AlertDialogDescription>
                                    {session.workspaceType === 'worktree' ? (
                                      <>
                                        This will permanently delete this session and <span className="font-medium">force delete</span> its worktree.
                                        <br />
                                        <span className="font-mono text-[11px]">{session.projectDir}</span>
                                        <br />
                                        Uncommitted changes in this worktree will be lost.
                                      </>
                                    ) : (
                                      'This will permanently delete this session.'
                                    )}
                                  </AlertDialogDescription>
                                </AlertDialogHeader>
                                <AlertDialogFooter>
                                  <AlertDialogCancel disabled={isSessionDeleting}>Cancel</AlertDialogCancel>
                                  <AlertDialogAction
                                    variant="destructive"
                                    disabled={isSessionDeleting}
                                    onClick={() => { void handleDeleteSession(session.id) }}
                                  >
                                    {isSessionDeleting ? (
                                      <>
                                        <Loader2 className="size-3 animate-spin" />
                                        Deleting...
                                      </>
                                    ) : 'Delete'}
                                  </AlertDialogAction>
                                </AlertDialogFooter>
                              </AlertDialogContent>
                            </AlertDialog>
                          </SidebarMenuSubItem>
                        )})}
                      </SidebarMenuSub>
                    </CollapsibleContent>
                  </Collapsible>
                )
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarSeparator />

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              onClick={() => navigate({ to: '/settings' })}
            >
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
