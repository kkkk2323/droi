// Contract between the Desktop Shell and the Local Client for opening a
// Workspace in another app (an editor, the file manager, a terminal). Only
// the Local Client can reach it; a Remote Client's Workspace is on another
// computer.

export interface OpenInApp {
  /** Stable id the Client remembers as its preferred target. */
  id: string
  label: string
  /** The app's icon as a data: URL, or null when the Shell could not read it. */
  icon: string | null
}

export interface OpenInBridge {
  /** The installed apps that can open a folder, in menu order. */
  list(): Promise<OpenInApp[]>
  /** Opens the directory in the app; rejects for an unknown app or a path that is not a directory. */
  open(path: string, appId: string): Promise<void>
}

export const OPEN_IN_IPC = {
  list: 'droi:open-in:list',
  open: 'droi:open-in:open',
} as const
