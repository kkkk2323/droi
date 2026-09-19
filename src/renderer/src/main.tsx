// Client: the single Droi user interface. The same bundle runs as the Local
// Client inside the Desktop Shell window and as the Remote Client in a phone
// browser; it speaks only the Daemon protocol. (See CONTEXT.md.)
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import './styles/global.css'

const root = document.getElementById('root')
if (!root) throw new Error('missing #root element')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
