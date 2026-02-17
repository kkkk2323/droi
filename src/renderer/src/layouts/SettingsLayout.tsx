import { Outlet, useNavigate, useParams } from '@tanstack/react-router'
import { useProjects } from '@/store'
import {
  SidebarProvider,
  SidebarInset,
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from '@/components/ui/sidebar'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { ArrowLeftIcon, KeyRoundIcon, FolderIcon, BugIcon, SettingsIcon, ChevronRightIcon } from 'lucide-react'

const NAV_ITEMS = [
  { id: 'general', label: 'General', icon: SettingsIcon, href: '/settings' },
  { id: 'api-keys', label: 'API Keys', icon: KeyRoundIcon, href: '/settings/keys' },
  { id: 'projects', label: 'Project Settings', icon: FolderIcon },
  { id: 'debug', label: 'Debug', icon: BugIcon, href: '/settings/debug' },
]

export function SettingsLayout() {
  const navigate = useNavigate()
  const projects = useProjects()
  const params = useParams({ strict: false })
  const activeProjectDir = params?.projectDir ? decodeURIComponent(params.projectDir) : ''

  const handleNav = (item: typeof NAV_ITEMS[number]) => {
    if (item.href) {
      navigate({ to: item.href })
    }
  }

  return (
    <SidebarProvider>
      <Sidebar variant="sidebar">
        <SidebarHeader >
          <SidebarMenu>
            <SidebarMenuItem className='!pt-8'>
              <SidebarMenuButton onClick={() => navigate({ to: '/' })}>
                <ArrowLeftIcon className="size-4" />
                <span>Back</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {NAV_ITEMS.map((item) => {
                  if (item.id === 'projects') {
                    return (
                      <Collapsible
                        key={item.id}
                        defaultOpen={!!activeProjectDir}
                        render={<SidebarMenuItem className="group/project" />}
                      >
                        <CollapsibleTrigger
                          render={
                            <SidebarMenuButton>
                              <item.icon className="size-4" />
                              <span>{item.label}</span>
                            </SidebarMenuButton>
                          }
                        />
                        <div className="absolute top-1.5 right-1 flex items-center">
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
                            {projects.map((project) => (
                              <SidebarMenuSubItem key={project.dir}>
                                <SidebarMenuSubButton
                                  isActive={activeProjectDir === project.dir}
                                  onClick={() => navigate({ to: '/settings/projects/$projectDir', params: { projectDir: encodeURIComponent(project.dir) } })}
                                >
                                  <span className="truncate">{project.name}</span>
                                </SidebarMenuSubButton>
                              </SidebarMenuSubItem>
                            ))}
                            {projects.length === 0 && (
                              <SidebarMenuSubItem>
                                <span className="text-xs text-muted-foreground">No projects</span>
                              </SidebarMenuSubItem>
                            )}
                          </SidebarMenuSub>
                        </CollapsibleContent>
                      </Collapsible>
                    )
                  }

                  return (
                    <SidebarMenuItem key={item.id}>
                      <SidebarMenuButton onClick={() => handleNav(item)}>
                        <item.icon className="size-4" />
                        <span>{item.label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  )
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-10 shrink-0 items-center gap-2 px-4 md:hidden">
          <button
            onClick={() => navigate({ to: '/' })}
            className="flex size-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ArrowLeftIcon className="size-4" />
          </button>
          <span className="text-sm font-medium">Settings</span>
        </header>
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}
