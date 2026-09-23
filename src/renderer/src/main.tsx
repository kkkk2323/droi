// Client: the single Droi user interface. The same bundle runs as the Local
// Client inside the Desktop Shell window and as the Remote Client in a phone
// browser; it speaks only the Daemon protocol. (See CONTEXT.md.)
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'
import { ConnectionProvider } from '@droi/daemon-layer/connection-context'
import { createDaemonConnection } from '@droi/daemon-layer/connection'
import { setPreferenceStorage } from '@droi/daemon-layer/local-preference'
import { browserEnvironment, resolveClientConfig } from './lib/client-config'
import { applyStoredTheme } from './lib/theme'
import { applyTextSize } from './lib/text-size'
import { applyFont } from './lib/font'
import 'streamdown/styles.css'
import './styles/global.css'

try {
  setPreferenceStorage(window.localStorage)
} catch {
  // Storage blocked: preferences hold in memory for this page.
}
applyStoredTheme()
applyTextSize()
applyFont()
const root = document.getElementById('root')
if (!root) throw new Error('missing #root element')

const connection = createDaemonConnection(resolveClientConfig(browserEnvironment()))
connection.start()
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
})

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ConnectionProvider connection={connection}>
        <App />
      </ConnectionProvider>
    </QueryClientProvider>
  </StrictMode>,
)
