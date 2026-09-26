// The Client's side of the Gateway's Scratch Workspace requests (ADR 0008).
import {
  GATEWAY_SCRATCH_PATH,
  GATEWAY_SCRATCH_RESTORE_PATH,
  GATEWAY_SCRATCH_TRASH_PATH,
  gatewayScratchUrl,
  type ScratchWorkspaceCreated,
} from './gateway'

export interface ScratchWorkspaces {
  /** A fresh folder on the computer to start a Session in. */
  create(): Promise<string>
  /** Moves the folder to the Trash; an empty one is simply removed. */
  trash(path: string): Promise<void>
  /** Recreates the folder if it is gone. */
  restore(path: string): Promise<void>
}

export function createScratchWorkspaces(
  config: { gatewayUrl: string; pairingToken: string | null },
  fetchImpl: typeof fetch,
): ScratchWorkspaces {
  const post = async (endpoint: string, path?: string): Promise<Response> => {
    const response = await fetchImpl(
      gatewayScratchUrl(config.gatewayUrl, config.pairingToken ?? '', endpoint, path),
      { method: 'POST', cache: 'no-store' },
    )
    if (response.ok) return response
    const reason = await response
      .json()
      .then((body: { error?: unknown }) => (typeof body.error === 'string' ? body.error : null))
      .catch(() => null)
    throw new Error(reason ?? `The computer could not make a folder (HTTP ${response.status}).`)
  }
  return {
    async create() {
      const body = (await (await post(GATEWAY_SCRATCH_PATH)).json()) as ScratchWorkspaceCreated
      return body.path
    },
    async trash(path) {
      await post(GATEWAY_SCRATCH_TRASH_PATH, path)
    },
    async restore(path) {
      await post(GATEWAY_SCRATCH_RESTORE_PATH, path)
    },
  }
}
