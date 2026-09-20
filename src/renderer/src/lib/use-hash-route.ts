import { useCallback, useSyncExternalStore } from 'react'

// Routing is a single `#/s/<sessionId>` fragment so a Remote Client reload or
// a shared link lands on the same Session. The `#pair=` fragment is consumed
// and removed before this ever runs (see client-config.ts).

export type Route =
  | { name: 'home' }
  /** `workspace` preselects where the Session will work (from a sidebar group). */
  | { name: 'new'; workspace?: string }
  | { name: 'settings' }
  | { name: 'session'; sessionId: string }

export function parseRoute(hash: string): Route {
  if (hash === '#/new') return { name: 'new' }
  if (hash.startsWith('#/new?')) {
    const workspace = new URLSearchParams(hash.slice('#/new?'.length)).get('ws')
    return workspace ? { name: 'new', workspace } : { name: 'new' }
  }
  if (hash === '#/settings') return { name: 'settings' }
  const match = /^#\/s\/([^/?#]+)/.exec(hash)
  return match?.[1]
    ? { name: 'session', sessionId: decodeURIComponent(match[1]) }
    : { name: 'home' }
}

export function routeHash(route: Route): string {
  switch (route.name) {
    case 'session':
      return `#/s/${encodeURIComponent(route.sessionId)}`
    case 'new':
      return route.workspace
        ? `#/new?${new URLSearchParams({ ws: route.workspace }).toString()}`
        : '#/new'
    case 'settings':
      return '#/settings'
    case 'home':
      return '#/'
  }
}

export function useHashRoute(): [Route, (route: Route) => void] {
  const subscribe = useCallback((listener: () => void) => {
    window.addEventListener('hashchange', listener)
    return () => window.removeEventListener('hashchange', listener)
  }, [])
  const hash = useSyncExternalStore(
    subscribe,
    () => window.location.hash,
    () => '',
  )
  const navigate = useCallback((route: Route) => {
    window.location.hash = routeHash(route)
  }, [])
  return [parseRoute(hash), navigate]
}
