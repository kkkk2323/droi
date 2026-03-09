import {
  createRouter,
  createRoute,
  createRootRoute,
  createMemoryHistory,
  createBrowserHistory,
  Outlet,
} from '@tanstack/react-router'
import { RootLayout } from './layouts/RootLayout'
import { SettingsLayout } from './layouts/SettingsLayout'
import { ChatPage } from './pages/ChatPage'
import { MissionPage } from './pages/MissionPage'
import { SettingsPage } from './pages/SettingsPage'
import { KeysPage } from './pages/KeysPage'
import { ProjectSettingsPage } from './pages/ProjectSettingsPage'
import { DebugSettingsPage } from './pages/DebugSettingsPage'

const rootRoute = createRootRoute({
  component: Outlet,
})

const chatLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'chat-layout',
  component: RootLayout,
})

export const chatRoute = createRoute({
  getParentRoute: () => chatLayoutRoute,
  path: '/',
  component: ChatPage,
})

export const missionRoute = createRoute({
  getParentRoute: () => chatLayoutRoute,
  path: '/mission',
  component: MissionPage,
})

const settingsLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'settings-layout',
  component: SettingsLayout,
})

export const settingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings',
  component: SettingsPage,
})

export const keysSettingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings/keys',
  component: KeysPage,
})

export const projectSettingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings/projects/$projectDir',
  component: ProjectSettingsPage,
})

export const debugSettingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings/debug',
  component: DebugSettingsPage,
})

const routeTree = rootRoute.addChildren([
  chatLayoutRoute.addChildren([chatRoute, missionRoute]),
  settingsLayoutRoute.addChildren([
    settingsRoute,
    keysSettingsRoute,
    projectSettingsRoute,
    debugSettingsRoute,
  ]),
])

const isElectron = typeof window !== 'undefined' && Boolean((window as any).droid)
const history = isElectron ? createMemoryHistory({ initialEntries: ['/'] }) : createBrowserHistory()

export const router = createRouter({
  routeTree,
  history,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
