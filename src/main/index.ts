// Desktop Shell: the installed desktop application. It starts the Daemon, opens a
// window for the Local Client, and hosts the Gateway. It holds no conversation
// state. (See CONTEXT.md.)
import { app, BrowserWindow, ipcMain, safeStorage, shell } from 'electron'
import { spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { join } from 'node:path'
import { electronApp, is, optimizer } from '@electron-toolkit/utils'
import { DaemonSupervisor } from './daemon/daemon-supervisor'
import { locateDroid } from './daemon/locate-droid'
import { startGateway, type ClientSource, type Gateway } from './gateway/gateway'
import {
  createShellSettingsStore,
  type SettingsCipher,
  type ShellSettingsStore,
} from './shell-settings'
import { SHELL_ARG_GATEWAY_URL, SHELL_ARG_PAIRING_TOKEN } from '../shared/shell-args'
import {
  SHELL_IPC,
  type PairingInfo,
  type ShellSettingsPatch,
  type ShellSettingsSnapshot,
} from '../shared/shell-settings'

// Tests point the Shell at a throwaway profile so they never touch real settings.
const userDataOverride = process.env['DROI_USER_DATA_DIR']
if (userDataOverride) app.setPath('userData', userDataOverride)

// Known only to this launch's window; resetting the Pairing Token leaves it alone.
const localToken = randomBytes(24).toString('base64url')

// Both need Electron to be ready: safeStorage reports encryption unavailable
// before `ready` on Windows and Linux, which would lock the store into its
// fallback cipher for good.
let settings: ShellSettingsStore
let daemon: DaemonSupervisor
let gateway: Gateway | null = null

function createDaemonSupervisor(): DaemonSupervisor {
  const supervisor = new DaemonSupervisor({
    spawn: (port) => {
      const droidPath = locateDroid({ override: settings.settings.droidPath })
      if (!droidPath) throw new Error('droid executable not found')
      const apiKey = settings.getApiKey()
      const baseUrl = settings.settings.factoryApiBaseUrl ?? process.env['FACTORY_API_BASE_URL']
      // The Daemon must run as the same Factory user whose key the Gateway
      // injects, otherwise it rejects every authenticate (see ADR 0003). A base
      // URL routes its Factory traffic through a local proxy such as droid-proxy.
      return spawn(
        droidPath,
        [
          'daemon',
          '--host',
          '127.0.0.1',
          '--port',
          String(port),
          '--parent-pid',
          String(process.pid),
        ],
        {
          stdio: 'ignore',
          env: {
            ...process.env,
            ...(apiKey ? { FACTORY_API_KEY: apiKey } : {}),
            ...(baseUrl ? { FACTORY_API_BASE_URL: baseUrl } : {}),
          },
        },
      )
    },
  })
  supervisor.on('state', (state) => console.log('[daemon]', JSON.stringify(state)))
  return supervisor
}

function clientSource(): ClientSource {
  const devUrl = process.env['ELECTRON_RENDERER_URL']
  if (is.dev && devUrl) return { kind: 'proxy', target: new URL(devUrl) }
  return { kind: 'static', dir: join(import.meta.dirname, '../renderer') }
}

function createWindow(gatewayUrl: string): void {
  const window = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 720,
    minHeight: 480,
    show: false,
    autoHideMenuBar: true,
    backgroundColor: '#1c1c1e',
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    webPreferences: {
      preload: join(import.meta.dirname, '../preload/index.mjs'),
      sandbox: false,
      additionalArguments: [
        `${SHELL_ARG_GATEWAY_URL}=${gatewayUrl}`,
        `${SHELL_ARG_PAIRING_TOKEN}=${localToken}`,
      ],
    },
  })

  window.on('ready-to-show', () => window.show())
  window.webContents.setWindowOpenHandler(({ url }) => {
    void shell.openExternal(url)
    return { action: 'deny' }
  })
  void window.loadURL(gatewayUrl)
}

function snapshot(): ShellSettingsSnapshot {
  const fromEnv = Boolean(process.env['FACTORY_API_KEY'])
  return {
    remoteAccess: settings.settings.remoteAccess,
    droidPath: settings.settings.droidPath,
    factoryApiBaseUrl: settings.settings.factoryApiBaseUrl,
    factoryApiBaseUrlFromEnvironment: process.env['FACTORY_API_BASE_URL'] ?? null,
    hasApiKey: settings.getApiKey() !== null,
    apiKeyFromEnvironment: fromEnv,
    droidFound: locateDroid({ override: settings.settings.droidPath }),
    version: app.getVersion(),
  }
}

function pairing(): PairingInfo {
  const lanAddresses = gateway?.lanAddresses ?? []
  const port = gateway?.port ?? 0
  const first = lanAddresses[0]
  return {
    link: first ? `http://${first}:${port}/#pair=${settings.settings.pairingToken}` : null,
    lanAddresses,
    port,
  }
}

function broadcastChange(): void {
  for (const window of BrowserWindow.getAllWindows()) {
    window.webContents.send(SHELL_IPC.changed)
  }
}

function registerIpc(): void {
  ipcMain.handle(SHELL_IPC.get, () => snapshot())
  ipcMain.handle(SHELL_IPC.getPairing, () => pairing())
  ipcMain.handle(SHELL_IPC.update, async (_event, patch: ShellSettingsPatch) => {
    const before = settings.settings
    settings.update({
      ...(patch.remoteAccess !== undefined ? { remoteAccess: patch.remoteAccess } : {}),
      ...(patch.droidPath !== undefined ? { droidPath: patch.droidPath || null } : {}),
      ...(patch.factoryApiBaseUrl !== undefined
        ? { factoryApiBaseUrl: patch.factoryApiBaseUrl?.trim() || null }
        : {}),
    })
    const after = settings.settings
    if (patch.remoteAccess !== undefined && after.remoteAccess !== before.remoteAccess) {
      await gateway?.setRemoteAccess(after.remoteAccess)
    }
    // The Daemon reads its path and environment at spawn only.
    if (
      after.droidPath !== before.droidPath ||
      after.factoryApiBaseUrl !== before.factoryApiBaseUrl
    ) {
      await restartDaemon()
    }
    broadcastChange()
    return snapshot()
  })
  ipcMain.handle(SHELL_IPC.setApiKey, async (_event, apiKey: string | null) => {
    settings.setApiKey(apiKey?.trim() || null)
    // The Daemon inherits the key at spawn, so a new key needs a new Daemon.
    await restartDaemon()
    broadcastChange()
    return snapshot()
  })
  ipcMain.handle(SHELL_IPC.resetPairingToken, () => {
    settings.resetPairingToken()
    gateway?.revoke('pairing')
    broadcastChange()
    return pairing()
  })
}

async function restartDaemon(): Promise<void> {
  await daemon.stop()
  daemon.start()
}

void app.whenReady().then(async () => {
  electronApp.setAppUserModelId('com.droi.app')
  app.on('browser-window-created', (_, window) => optimizer.watchWindowShortcuts(window))

  settings = createShellSettingsStore({
    file: join(app.getPath('userData'), 'settings.json'),
    cipher: safeStorageCipher(),
  })
  daemon = createDaemonSupervisor()
  registerIpc()
  daemon.start()
  gateway = await startGateway({
    port: 0,
    remoteAccess: settings.settings.remoteAccess,
    getDaemonUrl: () => daemon.daemonUrl,
    getPairingToken: () => settings.settings.pairingToken,
    getLocalToken: () => localToken,
    getApiKey: () => settings.getApiKey(),
    getMeta: () => ({
      app: 'Droi',
      version: app.getVersion(),
      remoteAccess: gateway?.remoteAccess ?? false,
    }),
    client: clientSource(),
  })

  createWindow(gateway.url)
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0 && gateway) createWindow(gateway.url)
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

let quitting = false
app.on('before-quit', (event) => {
  if (quitting) return
  quitting = true
  event.preventDefault()
  void Promise.allSettled([daemon?.stop(), gateway?.close()]).then(() => app.quit())
})

function safeStorageCipher(): SettingsCipher {
  if (!safeStorage.isEncryptionAvailable()) {
    // Without OS key storage we still avoid clear text; this is obfuscation
    // only, and the settings file is created mode 0600.
    return {
      encrypt: (plain) => Buffer.from(plain).toString('base64'),
      decrypt: (stored) => Buffer.from(stored, 'base64').toString(),
    }
  }
  return {
    encrypt: (plain) => safeStorage.encryptString(plain).toString('base64'),
    decrypt: (stored) => safeStorage.decryptString(Buffer.from(stored, 'base64')),
  }
}
