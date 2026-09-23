import type { DroiShell } from './index'

declare global {
  interface Window {
    // Present only in the Local Client; absent in a Remote Client's browser.
    droiShell?: DroiShell
  }
}
