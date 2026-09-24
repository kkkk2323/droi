// Desktop Shell: the installed desktop application. It starts the Daemon, opens a
// window for the Local Client, and hosts the Gateway. It holds no conversation
// state. (See CONTEXT.md.)
import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  Menu,
  nativeImage,
  net,
  Notification,
  safeStorage,
  shell,
} from 'electron'
import { execFile, spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { existsSync } from 'node:fs'
import { stat } from 'node:fs/promises'
// Electron's fs treats .asar files as directories; original-fs sees the file.
import * as originalFs from 'original-fs'
import { homedir } from 'node:os'
import { dirname, isAbsolute, join } from 'node:path'
import { electronApp, is, optimizer } from '@electron-toolkit/utils'
import { DaemonSupervisor } from './daemon/daemon-supervisor'
import { locateDroid } from './daemon/locate-droid'
import { createCliLoginReader, readRegistration, type CliLoginReader } from './cli-login'
import { createFactoryAuth, type FactoryAuth } from './factory-auth'
import {
  startGateway,
  type ClientSource,
  type Gateway,
  type GatewayCredential,
  type GatewayOptions,
} from './gateway/gateway'
import { builtinSoundPath, readSoundAsDataUrl, SOUND_FILE_EXTENSIONS } from './alert-sounds'
import { locateOpenInApps, type InstalledApp } from './open-in'
import { computerName } from './computer-name'
import { createShellSettingsStore, pairingHostOf, type ShellSettingsStore } from './shell-settings'
import { createUpdater, type Updater } from './updater'
import { ALERTS_IPC, type AlertNotification } from '../shared/alerts'
import { OPEN_IN_IPC, type OpenInApp } from '../shared/open-in'
import { SHELL_ARG_GATEWAY_URL, SHELL_ARG_PAIRING_TOKEN } from '../shared/shell-args'
import {
  SHELL_IPC,
  type LoginState,
  type PairingInfo,
  type ShellSettingsPatch,
  type ShellSettingsSnapshot,
} from '../shared/shell-settings'

// Tests point the Shell at a throwaway profile so they never touch real settings.
const userDataOverride = process.env['DROI_USER_DATA_DIR']
if (userDataOverride) app.setPath('userData', userDataOverride)

// Known only to this launch's window; resetting the Pairing Token leaves it alone.
const localToken = randomBytes(24).toString('base64url')

// Both need Electron to be ready.
let settings: ShellSettingsStore
let auth: FactoryAuth
let cliLogin: CliLoginReader
let daemon: DaemonSupervisor
let gateway: Gateway | null = null
let updater: Updater

const DEFAULT_FACTORY_API_BASE_URL = 'https://api.factory.ai'
const PREFERRED_GATEWAY_PORT = 41_417
const UPDATE_REPOSITORY = 'kkkk2323/droi'
/** A launch checks for a Release after settling; only a packaged app can swap its archive. */
const UPDATE_CHECK_DELAY_MS = 15_000

function createShellUpdater(): Updater {
  return createUpdater({
    currentVersion: app.getVersion(),
    repository: UPDATE_REPOSITORY,
    releaseBaseUrl: process.env['DROI_UPDATE_BASE_URL'],
    asarPath: join(dirname(app.getAppPath()), 'app.asar'),
    downloadDir: app.getPath('userData'),
    fetch: (url) => net.fetch(url),
    fs: {
      createWriteStream: (p) => originalFs.createWriteStream(p),
      createReadStream: (p) => originalFs.createReadStream(p),
      copyFile: originalFs.promises.copyFile,
      rename: originalFs.promises.rename,
      unlink: originalFs.promises.unlink,
    },
  })
}

function factoryApiBaseUrl(): string {
  return (
    settings.settings.factoryApiBaseUrl ??
    process.env['FACTORY_API_BASE_URL'] ??
    DEFAULT_FACTORY_API_BASE_URL
  )
}

/** The `droid` CLI's home; the Daemon reads its login and registration from here. */
function factoryHome(): string {
  return process.env['FACTORY_HOME_OVERRIDE'] ?? join(homedir(), '.factory')
}

/**
 * What the Gateway authenticates Clients with. A sign-in done in Droi comes
 * first, then the `droid` CLI's own login (the Daemon runs as it anyway), and
 * an API key (stored or FACTORY_API_KEY) is the fallback for automation.
 */
async function gatewayCredential(): Promise<GatewayCredential | null> {
  const token = await auth.getAccessToken()
  if (token) return { token }
  const cli = await cliLogin.read()
  if (cli) return { token: cli.accessToken }
  const apiKey = settings.getApiKey()
  return apiKey ? { apiKey } : null
}

/** Droi's own sign-in when there is one, else the CLI's login. */
async function loginState(): Promise<LoginState> {
  if (auth.state.status !== 'signed-out') return auth.state
  const cli = await cliLogin.read()
  return cli ? { status: 'signed-in', account: cli.account, source: 'cli' } : auth.state
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

async function snapshot(): Promise<ShellSettingsSnapshot> {
  const fromEnv = Boolean(process.env['FACTORY_API_KEY'])
  const login = await loginState()
  return {
    login,
    daemonIdentity: readRegistration(factoryHome()),
    hasCredential: login.status === 'signed-in' || settings.getApiKey() !== null,
    remoteAccess: settings.settings.remoteAccess,
    droidPath: settings.settings.droidPath,
    factoryApiBaseUrl: settings.settings.factoryApiBaseUrl,
    factoryApiBaseUrlFromEnvironment: process.env['FACTORY_API_BASE_URL'] ?? null,
    appendSystemPrompt: settings.settings.appendSystemPrompt,
    pairingHost: settings.settings.pairingHost,
    hasApiKey: settings.getApiKey() !== null,
    apiKeyFromEnvironment: fromEnv,
    droidFound: locateDroid({ override: settings.settings.droidPath }),
    version: app.getVersion(),
    update: updater.state,
  }
}

function pairing(): PairingInfo {
  const lanAddresses = gateway?.lanAddresses ?? []
  const port = gateway?.port ?? 0
  const first = lanAddresses[0]
  // A pairing host still needs the Gateway on a LAN address to answer it.
  const host = first ? (settings.settings.pairingHost ?? first) : null
  return {
    link: host ? `http://${host}:${port}/#pair=${settings.settings.pairingToken}` : null,
    lanAddresses,
    port,
  }
}

function broadcastChange(): void {
  for (const window of BrowserWindow.getAllWindows()) {
    window.webContents.send(SHELL_IPC.changed)
  }
}

// Looked up once per launch, like Waku. app.getFileIcon answers a generic
// folder for an .app bundle; the thumbnail is the app's own icon. 32px so the
// 16px button stays sharp on a Retina screen.
let openInApps: Promise<Array<InstalledApp & OpenInApp>> | null = null

function installedOpenInApps(): Promise<Array<InstalledApp & OpenInApp>> {
  openInApps ??= Promise.all(
    locateOpenInApps({ platform: process.platform, homedir: homedir(), exists: existsSync }).map(
      async (found) => ({
        ...found,
        icon: await nativeImage
          .createThumbnailFromPath(found.appPath, { width: 32, height: 32 })
          .then(
            (image) => image.toDataURL(),
            () => null,
          ),
      }),
    ),
  )
  return openInApps
}

async function openInApp(path: unknown, appId: unknown): Promise<void> {
  const target = (await installedOpenInApps()).find((found) => found.id === appId)
  if (!target) throw new Error(`Unknown app: ${String(appId)}`)
  const isDirectory =
    typeof path === 'string' &&
    isAbsolute(path) &&
    (await stat(path).then(
      (entry) => entry.isDirectory(),
      () => false,
    ))
  if (!isDirectory) throw new Error(`Not a directory: ${String(path)}`)
  await new Promise<void>((resolve, reject) => {
    execFile('open', ['-a', target.appPath, path], (error) => (error ? reject(error) : resolve()))
  })
}

// A notification that is collected before it is clicked loses its click handler.
const shownNotifications = new Set<Notification>()

function showAlert(window: BrowserWindow | null, alert: AlertNotification): void {
  if (!Notification.isSupported()) return
  // The Client plays its own sound for the same moment.
  const notification = new Notification({
    title: String(alert.title),
    body: String(alert.body),
    silent: true,
  })
  const sessionId = String(alert.sessionId)
  notification.on('click', () => {
    shownNotifications.delete(notification)
    if (!window || window.isDestroyed()) return
    if (window.isMinimized()) window.restore()
    window.show()
    window.focus()
    window.webContents.send(ALERTS_IPC.notificationClicked, sessionId)
  })
  notification.on('close', () => shownNotifications.delete(notification))
  shownNotifications.add(notification)
  notification.show()
}

function registerIpc(): void {
  ipcMain.handle(ALERTS_IPC.builtinSound, (_event, name: unknown) => {
    const path = builtinSoundPath(factoryHome(), name)
    return path ? readSoundAsDataUrl(path) : null
  })
  ipcMain.handle(ALERTS_IPC.readSoundFile, (_event, path: unknown) => readSoundAsDataUrl(path))
  ipcMain.handle(ALERTS_IPC.pickSoundFile, async (event) => {
    const window = BrowserWindow.fromWebContents(event.sender)
    const options = {
      properties: ['openFile' as const],
      filters: [{ name: 'Audio', extensions: SOUND_FILE_EXTENSIONS }],
    }
    const picked = window
      ? await dialog.showOpenDialog(window, options)
      : await dialog.showOpenDialog(options)
    return picked.canceled ? null : (picked.filePaths[0] ?? null)
  })
  ipcMain.handle(ALERTS_IPC.notify, (event, alert: AlertNotification) =>
    showAlert(BrowserWindow.fromWebContents(event.sender), alert),
  )
  ipcMain.handle(OPEN_IN_IPC.list, async (): Promise<OpenInApp[]> =>
    (await installedOpenInApps()).map(({ id, label, icon }) => ({ id, label, icon })),
  )
  ipcMain.handle(OPEN_IN_IPC.open, (_event, path: unknown, appId: unknown) =>
    openInApp(path, appId),
  )
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
      // Read by the Gateway at each Session start; the Daemon needs no restart.
      ...(patch.appendSystemPrompt !== undefined
        ? { appendSystemPrompt: patch.appendSystemPrompt?.trim() || null }
        : {}),
      ...(patch.pairingHost !== undefined ? { pairingHost: pairingHostOf(patch.pairingHost) } : {}),
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
  ipcMain.handle(SHELL_IPC.checkForUpdate, async () => {
    await updater.check()
    return snapshot()
  })
  ipcMain.handle(SHELL_IPC.installUpdate, async () => {
    await updater.install()
    return snapshot()
  })
  ipcMain.handle(SHELL_IPC.relaunch, () => {
    app.relaunch()
    app.quit()
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
    decryptLegacy: legacySafeStorageDecrypt(),
  })
  auth = createFactoryAuth({
    load: () => settings.getLogin(),
    save: (serialized) => settings.setLogin(serialized),
    factoryApiBaseUrl: factoryApiBaseUrl(),
  })
  cliLogin = createCliLoginReader({ factoryHome: factoryHome() })
  daemon = createDaemonSupervisor()
  updater = createShellUpdater()
  updater.on('change', broadcastChange)
  if (app.isPackaged && !process.env['DROI_NO_UPDATE_CHECK']) {
    setTimeout(() => void updater.check(), UPDATE_CHECK_DELAY_MS).unref()
  }
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
  // The Local Client's origin is this port, and its localStorage (theme,
  // favourites, sidebar) lives under that origin; a fresh port every launch
  // would wipe it. Fall back to an ephemeral port only when ours is taken.
  const name = computerName()
  const gatewayOptions = (port: number): GatewayOptions => ({
    port,
    remoteAccess: settings.settings.remoteAccess,
    getDaemonUrl: () => daemon.daemonUrl,
    getPairingToken: () => settings.settings.pairingToken,
    getLocalToken: () => localToken,
    getCredential: gatewayCredential,
    getAppendSystemPrompt: () => settings.settings.appendSystemPrompt,
    getMeta: () => ({
      app: 'Droi',
      version: app.getVersion(),
      remoteAccess: gateway?.remoteAccess ?? false,
      name,
      computerId: settings.settings.computerId,
    }),
    client: clientSource(),
  })
  gateway = await startGateway(gatewayOptions(PREFERRED_GATEWAY_PORT)).catch(() =>
    startGateway(gatewayOptions(0)),
  )

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

/**
 * Earlier versions kept secrets under Electron safeStorage; the store moves
 * them to the clear-text layout on first load. safeStorage's key is bound to
 * the app's code signature, so an ad-hoc-signed build cannot read what a
 * certificate-signed one wrote; that just means signing in again.
 */
function legacySafeStorageDecrypt(): ((stored: string) => string) | undefined {
  if (!safeStorage.isEncryptionAvailable()) {
    return (stored) => Buffer.from(stored, 'base64').toString()
  }
  return (stored) => safeStorage.decryptString(Buffer.from(stored, 'base64'))
}
