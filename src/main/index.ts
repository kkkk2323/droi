// Desktop Shell: the installed desktop application. It starts the Daemon, opens a
// window for the Local Client, and hosts the Gateway. It holds no conversation
// state. (See CONTEXT.md.)
import { app, BrowserWindow, ipcMain, Menu, safeStorage, shell } from 'electron'
import { spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { electronApp, is, optimizer } from '@electron-toolkit/utils'
import { DaemonSupervisor } from './daemon/daemon-supervisor'
import { locateDroid } from './daemon/locate-droid'
import { createFactoryAuth, type FactoryAuth } from './factory-auth'
import {
  startGateway,
  type ClientSource,
  type Gateway,
  type GatewayCredential,
} from './gateway/gateway'
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
let auth: FactoryAuth
let daemon: DaemonSupervisor
let gateway: Gateway | null = null

const DEFAULT_FACTORY_API_BASE_URL = 'https://api.factory.ai'

function factoryApiBaseUrl(): string {
  return (
    settings.settings.factoryApiBaseUrl ??
    process.env['FACTORY_API_BASE_URL'] ??
    DEFAULT_FACTORY_API_BASE_URL
  )
}

/**
 * What the Gateway authenticates Clients with. The Factory login comes first;
 * an API key (stored or FACTORY_API_KEY) is the fallback for automation.
 */
async function gatewayCredential(): Promise<GatewayCredential | null> {
  const token = await auth.getAccessToken()
  if (token) return { token }
  const apiKey = settings.getApiKey()
  return apiKey ? { apiKey } : null
}

/** The `droid` CLI's login on this computer, which is who the Daemon runs as. */
function daemonIdentity(): ShellSettingsSnapshot['daemonIdentity'] {
  const factoryHome = process.env['FACTORY_HOME_OVERRIDE'] ?? join(homedir(), '.factory')
  try {
    const host = JSON.parse(readFileSync(join(factoryHome, 'host.json'), 'utf8')) as {
      computerRegistration?: { userId?: unknown; firestoreOrgId?: unknown }
    }
    const registration = host.computerRegistration
    if (typeof registration?.userId !== 'string') return null
    return {
      userId: registration.userId,
      orgId: typeof registration.firestoreOrgId === 'string' ? registration.firestoreOrgId : null,
    }
  } catch {
    return null
  }
}

function createDaemonSupervisor(): DaemonSupervisor {
  const supervisor = new DaemonSupervisor({
    spawn: (port) => {
      const droidPath = locateDroid({ override: settings.settings.droidPath })
      if (!droidPath) throw new Error('droid executable not found')
      // Signed in: the Daemon runs as the `droid` CLI's login, like `dp` does,
      // and the Gateway authenticates Clients with the Droi login (ADR 0005).
      // Without a login an API key has to serve both sides (ADR 0003). A base
      // URL routes the Daemon's Factory traffic through a proxy such as droid-proxy.
      const apiKey = auth.state.status === 'signed-in' ? null : settings.getApiKey()
      const baseUrl = settings.settings.factoryApiBaseUrl ?? process.env['FACTORY_API_BASE_URL']
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
    backgroundColor: '#f3f3f3',
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

/**
 * The standard menu, spelled out so the zoom shortcuts are the ones people
 * press: ⌘= / ⌘- / ⌘0. Electron's default View menu binds Zoom In to
 * ⌘⇧= (Plus) and leaves ⌘- unreliable on some layouts.
 */
function buildMenu(): Menu {
  const mac = process.platform === 'darwin'
  return Menu.buildFromTemplate([
    ...(mac ? [{ role: 'appMenu' as const }] : []),
    { role: 'fileMenu' },
    { role: 'editMenu' },
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'toggleDevTools' },
        { type: 'separator' },
        { role: 'resetZoom', accelerator: 'CommandOrControl+0' },
        { role: 'zoomIn', accelerator: 'CommandOrControl+=' },
        { role: 'zoomIn', accelerator: 'CommandOrControl+Plus', visible: false },
        { role: 'zoomOut', accelerator: 'CommandOrControl+-' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
      ],
    },
    { role: 'windowMenu' },
  ])
}

function snapshot(): ShellSettingsSnapshot {
  const fromEnv = Boolean(process.env['FACTORY_API_KEY'])
  return {
    login: auth.state,
    daemonIdentity: daemonIdentity(),
    hasCredential: auth.state.status === 'signed-in' || settings.getApiKey() !== null,
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
  ipcMain.handle(SHELL_IPC.signIn, async () => {
    const pending = await auth.signIn()
    void shell.openExternal(pending.verificationUriComplete)
    return snapshot()
  })
  ipcMain.handle(SHELL_IPC.cancelSignIn, () => {
    auth.cancelSignIn()
    return snapshot()
  })
  ipcMain.handle(SHELL_IPC.signOut, () => {
    auth.signOut()
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
  auth = createFactoryAuth({
    load: () => settings.getLogin(),
    save: (serialized) => settings.setLogin(serialized),
    factoryApiBaseUrl: factoryApiBaseUrl(),
  })
  daemon = createDaemonSupervisor()
  let wasSignedIn = auth.state.status === 'signed-in'
  auth.on('change', (state) => {
    broadcastChange()
    // Signing in moves the Daemon off the API key; the env is read at spawn.
    const signedIn = state.status === 'signed-in'
    if (signedIn !== wasSignedIn) void restartDaemon()
    wasSignedIn = signedIn
  })
  registerIpc()
  daemon.start()
  gateway = await startGateway({
    port: 0,
    remoteAccess: settings.settings.remoteAccess,
    getDaemonUrl: () => daemon.daemonUrl,
    getPairingToken: () => settings.settings.pairingToken,
    getLocalToken: () => localToken,
    getCredential: gatewayCredential,
    getMeta: () => ({
      app: 'Droi',
      version: app.getVersion(),
      remoteAccess: gateway?.remoteAccess ?? false,
    }),
    client: clientSource(),
  })

  Menu.setApplicationMenu(buildMenu())
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
