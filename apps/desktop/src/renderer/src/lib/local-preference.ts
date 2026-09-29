// Preferences only the web Client has; the shared ones live in the daemon layer.
import {
  createBooleanPreference,
  createStringListPreference,
  createStringPreference,
} from '@droi/daemon-layer/local-preference'

export const sidebarVisible = createBooleanPreference('droi.sidebar', true)
/** The app the header's "open in" button opens the Workspace in; the last one picked. */
export const openInApp = createStringPreference('droi.openInApp')
/** Project Memories the corner card already said are large, by Workspace and last consolidation. */
export const largeMemoriesNoticed = createStringListPreference('droi.largeMemoriesNoticed')
