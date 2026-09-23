// Which folder-capable apps are installed, for the Client's "open in" control.
// The catalog and its order follow Waku's: editors, the file manager,
// terminals, IDEs. macOS only; elsewhere the list is empty and the control
// stays hidden.
import { join } from 'node:path'

interface CatalogEntry {
  id: string
  label: string
  /** Bundle names this app ships under; the first one found wins. */
  bundles: string[]
}

const CATALOG: CatalogEntry[] = [
  { id: 'vscode', label: 'VS Code', bundles: ['Visual Studio Code.app'] },
  { id: 'cursor', label: 'Cursor', bundles: ['Cursor.app'] },
  { id: 'zed', label: 'Zed', bundles: ['Zed.app', 'Zed Preview.app'] },
  { id: 'finder', label: 'Finder', bundles: ['Finder.app'] },
  { id: 'terminal', label: 'Terminal', bundles: ['Terminal.app'] },
  { id: 'iterm2', label: 'iTerm2', bundles: ['iTerm.app'] },
  { id: 'kitty', label: 'Kitty', bundles: ['kitty.app'] },
  { id: 'ghostty', label: 'Ghostty', bundles: ['Ghostty.app'] },
  { id: 'warp', label: 'Warp', bundles: ['Warp.app'] },
  { id: 'xcode', label: 'Xcode', bundles: ['Xcode.app'] },
  { id: 'android-studio', label: 'Android Studio', bundles: ['Android Studio.app'] },
]

export interface InstalledApp {
  id: string
  label: string
  /** The .app bundle, handed to `open -a`. */
  appPath: string
}

export interface LocateAppsOptions {
  platform: NodeJS.Platform
  homedir: string
  exists: (path: string) => boolean
}

export function locateOpenInApps({ platform, homedir, exists }: LocateAppsOptions): InstalledApp[] {
  if (platform !== 'darwin') return []
  const folders = [
    '/Applications',
    join(homedir, 'Applications'),
    '/System/Applications',
    '/System/Applications/Utilities',
    '/System/Library/CoreServices',
  ]
  const found: InstalledApp[] = []
  for (const entry of CATALOG) {
    const appPath = entry.bundles
      .flatMap((bundle) => folders.map((folder) => join(folder, bundle)))
      .find(exists)
    if (appPath) found.push({ id: entry.id, label: entry.label, appPath })
  }
  return found
}
