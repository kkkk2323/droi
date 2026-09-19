import React from 'react'
import ReactDOM from 'react-dom/client'
import { RouterProvider } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { router } from './router'
import { AppInitializer } from './AppInitializer'
import 'streamdown/styles.css'
import './styles/global.css'

function getStartupBridge(): {
  getStartupMetrics?: () => Promise<Record<string, unknown>>
  markStartupMetric?: (params: { name: string; ts?: number }) => void
} | null {
  return typeof window !== 'undefined' ? ((window as any).droid ?? null) : null
}

async function refreshStartupMetrics() {
  const bridge = getStartupBridge()
  if (typeof bridge?.getStartupMetrics !== 'function') return
  try {
    ;(window as any).__droiStartupMetrics = await bridge.getStartupMetrics()
  } catch {
    // ignore startup metrics failures
  }
}

function markStartupMetric(name: string) {
  const bridge = getStartupBridge()
  if (typeof bridge?.markStartupMetric === 'function') {
    bridge.markStartupMetric({ name, ts: Date.now() })
  }
  void refreshStartupMetrics()
}

markStartupMetric('rendererBoot')

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 30,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

function Bootstrap() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppInitializer>
        <RouterProvider router={router} />
      </AppInitializer>
    </QueryClientProvider>
  )
}

void refreshStartupMetrics()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Bootstrap />
  </React.StrictMode>,
)

requestAnimationFrame(() => {
  if ((window as any).__droiStartupFirstFrameRecorded) return
  ;(window as any).__droiStartupFirstFrameRecorded = true
  markStartupMetric('firstFrame')
})
