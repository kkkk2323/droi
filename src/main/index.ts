// Desktop Shell: the installed desktop application. It starts the Daemon, opens a
// window for the Local Client, and hosts the Gateway. It holds no conversation
// state. (See CONTEXT.md.)
import { app, BrowserWindow, safeStorage, shell } from 'electron'
import { spawn } from 'node:child_process'
import { join } from 'node:path'
import { electronApp, is, optimizer } from '@electron-toolkit/utils'
import { DaemonSupervisor } from './daemon/daemon-supervisor'
import { locateDroid } from './daemon/locate-droid'
import { startGateway, type ClientSource, type Gateway } from './gateway/gateway'
import { createShellSettingsStore, type SettingsCipher } from './shell-settings'
import { SHELL_ARG_GATEWAY_URL, SHELL_ARG_PAIRING_TOKEN } from '../shared/shell-args'

const settings = createShellSettingsStore({
  file: join(app.getPath('userData'), 'settings.json'),
  cipher: safeStorageCipher(),
})

const daemon = new DaemonSupervisor({
  spawn: (port) => {
    const droidPath = locateDroid({ override: settings.settings.droidPath })
    if (!droidPath) throw new Error('droid executable not found')
    const apiKey = settings.getApiKey()
    // The Daemon must run as the same Factory user whose key the Gateway
    // injects, otherwise it rejects every authenticate (see ADR 0003).
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
        env: { ...process.env, ...(apiKey ? { FACTORY_API_KEY: apiKey } : {}) },
      },
    )
  },
})

daemon.on('state', (state) => console.log('[daemon]', JSON.stringify(state)))

let gateway: Gateway | null = null

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
        `${SHELL_ARG_PAIRING_TOKEN}=${settings.settings.pairingToken}`,
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

void app.whenReady().then(async () => {
  electronApp.setAppUserModelId('com.droi.app')
  app.on('browser-window-created', (_, window) => optimizer.watchWindowShortcuts(window))

  daemon.start()
  gateway = await startGateway({
    port: 0,
    remoteAccess: settings.settings.remoteAccess,
    getDaemonUrl: () => daemon.daemonUrl,
    getPairingToken: () => settings.settings.pairingToken,
    getApiKey: () => settings.getApiKey(),
    getMeta: () => ({
      app: 'Droi',
      version: app.getVersion(),
      remoteAccess: settings.settings.remoteAccess,
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
  void Promise.allSettled([daemon.stop(), gateway?.close()]).then(() => app.quit())
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
