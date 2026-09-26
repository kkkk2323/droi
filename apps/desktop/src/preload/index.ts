// Preload: the only bridge between the Desktop Shell and the Local Client. It
// exposes the Gateway URL, this window's token, the platform, the Shell
// settings calls, opening a Workspace in another app, alert sounds and
// notifications, and the path of a pasted or dropped file. Conversation data
// never crosses here; it goes through the Gateway.
import { contextBridge, ipcRenderer, webUtils } from 'electron'
import { readShellArg, SHELL_ARG_GATEWAY_URL, SHELL_ARG_PAIRING_TOKEN } from '../shared/shell-args'
import { ALERTS_IPC, type AlertsBridge } from '../shared/alerts'
import { OPEN_IN_IPC, type OpenInBridge } from '../shared/open-in'
import { SHELL_IPC, type ShellSettingsBridge } from '../shared/shell-settings'

const settings: ShellSettingsBridge = {
  get: () => ipcRenderer.invoke(SHELL_IPC.get),
  update: (patch) => ipcRenderer.invoke(SHELL_IPC.update, patch),
  setApiKey: (apiKey) => ipcRenderer.invoke(SHELL_IPC.setApiKey, apiKey),
  restartDaemon: () => ipcRenderer.invoke(SHELL_IPC.restartDaemon),
  signIn: () => ipcRenderer.invoke(SHELL_IPC.signIn),
  cancelSignIn: () => ipcRenderer.invoke(SHELL_IPC.cancelSignIn),
  signOut: () => ipcRenderer.invoke(SHELL_IPC.signOut),
  resetPairingToken: () => ipcRenderer.invoke(SHELL_IPC.resetPairingToken),
  getPairing: () => ipcRenderer.invoke(SHELL_IPC.getPairing),
  checkForUpdate: () => ipcRenderer.invoke(SHELL_IPC.checkForUpdate),
  installUpdate: () => ipcRenderer.invoke(SHELL_IPC.installUpdate),
  relaunch: () => ipcRenderer.invoke(SHELL_IPC.relaunch),
  onChange: (listener) => {
    const handler = () => listener()
    ipcRenderer.on(SHELL_IPC.changed, handler)
    return () => ipcRenderer.off(SHELL_IPC.changed, handler)
  },
}

const openIn: OpenInBridge = {
  list: () => ipcRenderer.invoke(OPEN_IN_IPC.list),
  open: (path, appId) => ipcRenderer.invoke(OPEN_IN_IPC.open, path, appId),
}

const alerts: AlertsBridge = {
  builtinSound: (name) => ipcRenderer.invoke(ALERTS_IPC.builtinSound, name),
  pickSoundFile: () => ipcRenderer.invoke(ALERTS_IPC.pickSoundFile),
  readSoundFile: (path) => ipcRenderer.invoke(ALERTS_IPC.readSoundFile, path),
  notify: (notification) => ipcRenderer.invoke(ALERTS_IPC.notify, notification),
  onNotificationClick: (listener) => {
    const handler = (_event: unknown, sessionId: string) => listener(sessionId)
    ipcRenderer.on(ALERTS_IPC.notificationClicked, handler)
    return () => ipcRenderer.off(ALERTS_IPC.notificationClicked, handler)
  },
}

const droiShell = {
  gatewayUrl: readShellArg(process.argv, SHELL_ARG_GATEWAY_URL) ?? '',
  pairingToken: readShellArg(process.argv, SHELL_ARG_PAIRING_TOKEN) ?? '',
  platform: process.platform,
  settings,
  openIn,
  alerts,
  /** The file's path on this computer; empty for a File not backed by one. */
  pathForFile: (file: File) => webUtils.getPathForFile(file),
}

export type DroiShell = typeof droiShell

contextBridge.exposeInMainWorld('droiShell', droiShell)
