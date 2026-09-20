// Preload: the only bridge between the Desktop Shell and the Local Client. It
// exposes the Gateway URL, this window's token, the platform, and the Shell
// settings calls. Conversation data never crosses here; it goes through the
// Gateway.
import { contextBridge, ipcRenderer } from 'electron'
import { readShellArg, SHELL_ARG_GATEWAY_URL, SHELL_ARG_PAIRING_TOKEN } from '../shared/shell-args'
import { SHELL_IPC, type ShellSettingsBridge } from '../shared/shell-settings'

const settings: ShellSettingsBridge = {
  get: () => ipcRenderer.invoke(SHELL_IPC.get),
  update: (patch) => ipcRenderer.invoke(SHELL_IPC.update, patch),
  setApiKey: (apiKey) => ipcRenderer.invoke(SHELL_IPC.setApiKey, apiKey),
  signIn: () => ipcRenderer.invoke(SHELL_IPC.signIn),
  cancelSignIn: () => ipcRenderer.invoke(SHELL_IPC.cancelSignIn),
  signOut: () => ipcRenderer.invoke(SHELL_IPC.signOut),
  resetPairingToken: () => ipcRenderer.invoke(SHELL_IPC.resetPairingToken),
  getPairing: () => ipcRenderer.invoke(SHELL_IPC.getPairing),
  onChange: (listener) => {
    const handler = () => listener()
    ipcRenderer.on(SHELL_IPC.changed, handler)
    return () => ipcRenderer.off(SHELL_IPC.changed, handler)
  },
}

const droiShell = {
  gatewayUrl: readShellArg(process.argv, SHELL_ARG_GATEWAY_URL) ?? '',
  pairingToken: readShellArg(process.argv, SHELL_ARG_PAIRING_TOKEN) ?? '',
  platform: process.platform,
  settings,
}

export type DroiShell = typeof droiShell

contextBridge.exposeInMainWorld('droiShell', droiShell)
