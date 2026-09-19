import { useCallback, useSyncExternalStore } from 'react'

// Routing is a single `#/s/<sessionId>` fragment so a Remote Client reload or
// a shared link lands on the same Session. The `#pair=` fragment is consumed
// and removed before this ever runs (see client-config.ts).

export type Route = { name: 'home' } | { name: 'session'; sessionId: string }

export function parseRoute(hash: string): Route {
  const match = /^#\/s\/([^/?#]+)/.exec(hash)
  return match?.[1]
    ? { name: 'session', sessionId: decodeURIComponent(match[1]) }
    : { name: 'home' }
}

export function routeHash(route: Route): string {
  return route.name === 'session' ? `#/s/${encodeURIComponent(route.sessionId)}` : '#/'
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
