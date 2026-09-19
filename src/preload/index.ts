// Preload: the only bridge between the Desktop Shell and the Local Client. It
// exposes exactly three values: the Gateway URL, the Pairing Token, and the
// platform. Conversation data never crosses here; it goes through the Gateway.
import { contextBridge } from 'electron'
import { readShellArg, SHELL_ARG_GATEWAY_URL, SHELL_ARG_PAIRING_TOKEN } from '../shared/shell-args'

const droiShell = {
  gatewayUrl: readShellArg(process.argv, SHELL_ARG_GATEWAY_URL) ?? '',
  pairingToken: readShellArg(process.argv, SHELL_ARG_PAIRING_TOKEN) ?? '',
  platform: process.platform,
}

export type DroiShell = typeof droiShell

contextBridge.exposeInMainWorld('droiShell', droiShell)
