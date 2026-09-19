// Client: the single Droi user interface. The same bundle runs as the Local
// Client inside the Desktop Shell window and as the Remote Client in a phone
// browser; it speaks only the Daemon protocol. (See CONTEXT.md.)
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { ConnectionProvider } from './daemon/connection-context'
import { createDaemonConnection } from './daemon/connection'
import { browserEnvironment, resolveClientConfig } from './lib/client-config'
import './styles/global.css'

const root = document.getElementById('root')
if (!root) throw new Error('missing #root element')

const connection = createDaemonConnection(resolveClientConfig(browserEnvironment()))
connection.start()

createRoot(root).render(
  <StrictMode>
    <ConnectionProvider connection={connection}>
      <App />
    </ConnectionProvider>
  </StrictMode>,
)
