// OMP engine sessions over the Gateway HTTP surface (not the Daemon protocol).

import {
  gatewayOmpAbortUrl,
  gatewayOmpModelUrl,
  gatewayOmpModelsUrl,
  gatewayOmpPromptUrl,
  gatewayOmpSessionUrl,
  gatewayOmpSessionsUrl,
  type OmpMessage,
  type OmpModel,
  type OmpSession,
} from './gateway'

export type { OmpMessage, OmpModel, OmpSession }

async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    if (body.error) return body.error
  } catch {
    /* ignore */
  }
  return res.statusText || `HTTP ${res.status}`
}

export async function listOmpSessions(
  gatewayHttpUrl: string,
  pairingToken: string,
): Promise<{ available: boolean; sessions: OmpSession[] }> {
  const res = await fetch(gatewayOmpSessionsUrl(gatewayHttpUrl, pairingToken))
  if (!res.ok) throw new Error(await readError(res))
  return (await res.json()) as { available: boolean; sessions: OmpSession[] }
}

export async function createOmpSession(
  gatewayHttpUrl: string,
  pairingToken: string,
  body: { cwd: string; model?: string; title?: string },
): Promise<OmpSession> {
  const res = await fetch(gatewayOmpSessionsUrl(gatewayHttpUrl, pairingToken), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(await readError(res))
  return (await res.json()) as OmpSession
}

export async function getOmpSession(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): Promise<{ session: OmpSession; messages: OmpMessage[] }> {
  const res = await fetch(gatewayOmpSessionUrl(gatewayHttpUrl, pairingToken, sessionId))
  if (!res.ok) throw new Error(await readError(res))
  return (await res.json()) as { session: OmpSession; messages: OmpMessage[] }
}

export async function promptOmpSession(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
  message: string,
): Promise<void> {
  const res = await fetch(gatewayOmpPromptUrl(gatewayHttpUrl, pairingToken), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId, message }),
  })
  if (!res.ok) throw new Error(await readError(res))
}

export async function abortOmpSession(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): Promise<void> {
  const res = await fetch(gatewayOmpAbortUrl(gatewayHttpUrl, pairingToken), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId }),
  })
  if (!res.ok) throw new Error(await readError(res))
}

export async function listOmpModels(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
): Promise<OmpModel[]> {
  const res = await fetch(gatewayOmpModelsUrl(gatewayHttpUrl, pairingToken, sessionId))
  if (!res.ok) throw new Error(await readError(res))
  const body = (await res.json()) as { models: OmpModel[] }
  return body.models ?? []
}

export async function setOmpModel(
  gatewayHttpUrl: string,
  pairingToken: string,
  sessionId: string,
  modelId: string,
): Promise<void> {
  const res = await fetch(gatewayOmpModelUrl(gatewayHttpUrl, pairingToken), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId, modelId }),
  })
  if (!res.ok) throw new Error(await readError(res))
}
