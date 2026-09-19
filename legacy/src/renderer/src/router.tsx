import React, { Suspense, lazy } from 'react'
import {
  createRouter,
  createRoute,
  createRootRoute,
  createMemoryHistory,
  createBrowserHistory,
  Outlet,
} from '@tanstack/react-router'
import { RootLayout } from './layouts/RootLayout'
import { ChatPage } from './pages/ChatPage'

const SettingsLayout = lazy(() =>
  import('./layouts/SettingsLayout').then((module) => ({ default: module.SettingsLayout })),
)
const MissionPage = lazy(() =>
  import('./pages/MissionPage').then((module) => ({ default: module.MissionPage })),
)
const SettingsPage = lazy(() =>
  import('./pages/SettingsPage').then((module) => ({ default: module.SettingsPage })),
)
const KeysPage = lazy(() =>
  import('./pages/KeysPage').then((module) => ({ default: module.KeysPage })),
)
const ProjectSettingsPage = lazy(() =>
  import('./pages/ProjectSettingsPage').then((module) => ({ default: module.ProjectSettingsPage })),
)
const DebugSettingsPage = lazy(() =>
  import('./pages/DebugSettingsPage').then((module) => ({ default: module.DebugSettingsPage })),
)

function renderLazy(Component: React.LazyExoticComponent<() => React.JSX.Element>) {
  return function LazyRouteComponent() {
    return (
      <Suspense fallback={null}>
        <Component />
      </Suspense>
    )
  }
}

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
  component: renderLazy(MissionPage),
})

const settingsLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'settings-layout',
  component: renderLazy(SettingsLayout),
})

export const settingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings',
  component: renderLazy(SettingsPage),
})

export const keysSettingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings/keys',
  component: renderLazy(KeysPage),
})

export const projectSettingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings/projects/$projectDir',
  component: renderLazy(ProjectSettingsPage),
})

export const debugSettingsRoute = createRoute({
  getParentRoute: () => settingsLayoutRoute,
  path: '/settings/debug',
  component: renderLazy(DebugSettingsPage),
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
