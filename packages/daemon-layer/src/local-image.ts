// Images the agent writes into its markdown as paths on the computer, such as
// a screenshot it took. A browser cannot load those: the Local Client's origin
// is not the filesystem and a Remote Client is on another device. The Daemon
// reads them instead (`daemon.get_workspace_file_content`), so every Client
// shows them over the connection it already has.
import { useQuery } from '@tanstack/react-query'
import { createContext, useContext } from 'react'
import { useDaemonConnection } from './connection-context'

/**
 * The absolute path on the computer an image `src` names, or null when the
 * browser can load it itself (http, data, blob, ...). Markdown sanitising has
 * already percent-encoded the path by the time it reaches a component.
 */
export function localImagePath(src: string): string | null {
  const trimmed = src.trim()
  if (!trimmed.startsWith('/') || trimmed.startsWith('//')) return null
  try {
    return decodeURIComponent(trimmed)
  } catch {
    return trimmed
  }
}

const ImageSessionContext = createContext<string | null>(null)

/** The Session the Daemon reads the images for. */
export const ImageSessionProvider = ImageSessionContext.Provider

export type LocalImage =
  | { status: 'loading' }
  | { status: 'ready'; src: string }
  | { status: 'missing' }

/** A data URL for an image file on the computer, read through the Daemon. */
export function useLocalImage(path: string): LocalImage {
  const { controller } = useDaemonConnection()
  const sessionId = useContext(ImageSessionContext)
  const query = useQuery({
    queryKey: ['local-image', sessionId, path],
    enabled: sessionId !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async () => {
      const file = await controller.getWorkspaceFileContent({
        sessionId: sessionId!,
        filePath: path,
        encoding: 'base64',
      })
      if (file.encoding !== 'base64' || !file.mimeType?.startsWith('image/')) {
        throw new Error(`${path} is not an image`)
      }
      return `data:${file.mimeType};base64,${file.content}`
    },
  })
  if (query.data) return { status: 'ready', src: query.data }
  if (query.isError || sessionId === null) return { status: 'missing' }
  return { status: 'loading' }
}
