// Preload: the only bridge between the Desktop Shell and the Local Client. It
// exposes just enough for the Client to know it runs inside the Desktop Shell;
// conversation data never crosses here, it goes through the Gateway.
import { contextBridge } from 'electron'

const droiShell = {
  platform: process.platform,
}

export type DroiShell = typeof droiShell

contextBridge.exposeInMainWorld('droiShell', droiShell)
