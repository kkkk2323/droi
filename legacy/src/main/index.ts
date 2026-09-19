import { app, shell, BrowserWindow, protocol, net, ipcMain } from 'electron'
import { join } from 'path'
import { pathToFileURL } from 'url'
import { electronApp, optimizer, is } from '@electron-toolkit/utils'
import { registerIpcHandlers } from './ipc/registerHandlers'
import { registerUpdaterIpc } from './updater/updaterIpc'
import { startApiServer } from '../server/apiServer.ts'
import { LocalDiagnostics } from '../backend/diagnostics/localDiagnostics.ts'
import { createAppStateStore } from '../backend/storage/appStateStore.ts'

const debugPort = process.env.ELECTRON_REMOTE_DEBUGGING_PORT
if (debugPort) {
  app.commandLine.appendSwitch('remote-debugging-port', debugPort)
}

let mainWindow: BrowserWindow | null = null
let ipcCtl: { cancelActiveRun: () => boolean; close?: () => Promise<void> } | null = null
let apiCtl: { close: () => Promise<void> } | null = null
const startupMetrics = {
  launchAt: Date.now(),
  whenReadyAt: null as number | null,
  windowCreatedAt: null as number | null,
  rendererLoadStartedAt: null as number | null,
  rendererDidFinishLoadAt: null as number | null,
  readyToShowAt: null as number | null,
  rendererMarks: {} as Record<string, number>,
}

function setStartupTimestamp(
  key:
    | 'whenReadyAt'
    | 'windowCreatedAt'
    | 'rendererLoadStartedAt'
    | 'rendererDidFinishLoadAt'
    | 'readyToShowAt',
  ts = Date.now(),
) {
  if (startupMetrics[key] === null) startupMetrics[key] = ts
}

function setRendererStartupMark(name: string, ts = Date.now()) {
  const normalized = String(name || '').trim()
  if (!normalized) return
  const prev = startupMetrics.rendererMarks[normalized]
  if (typeof prev !== 'number' || ts < prev) startupMetrics.rendererMarks[normalized] = ts
}

function getStartupMetricsSnapshot() {
  const launchAt = startupMetrics.launchAt
  const relative = Object.fromEntries(
    Object.entries({
      launchAt,
      whenReadyAt: startupMetrics.whenReadyAt,
      windowCreatedAt: startupMetrics.windowCreatedAt,
      rendererLoadStartedAt: startupMetrics.rendererLoadStartedAt,
      rendererDidFinishLoadAt: startupMetrics.rendererDidFinishLoadAt,
      readyToShowAt: startupMetrics.readyToShowAt,
      ...startupMetrics.rendererMarks,
    }).map(([key, value]) => [
      key,
      typeof value === 'number' && Number.isFinite(value) ? Math.max(0, value - launchAt) : null,
    ]),
  )

  return {
    launchAt,
    whenReadyAt: startupMetrics.whenReadyAt,
    windowCreatedAt: startupMetrics.windowCreatedAt,
    rendererLoadStartedAt: startupMetrics.rendererLoadStartedAt,
    rendererDidFinishLoadAt: startupMetrics.rendererDidFinishLoadAt,
    readyToShowAt: startupMetrics.readyToShowAt,
    rendererMarks: { ...startupMetrics.rendererMarks },
    relative,
  }
}

function readBool(name: string, def: boolean): boolean {
  const raw = (process.env[name] || '').trim().toLowerCase()
  if (!raw) return def
  return raw === '1' || raw === 'true' || raw === 'yes' || raw === 'on'
}

function parsePortFromUrl(raw: string | undefined): number | undefined {
  const s = (raw || '').trim()
  if (!s) return undefined
  try {
    const u = new URL(s)
    const p = u.port ? Number(u.port) : u.protocol === 'https:' ? 443 : 80
    return Number.isFinite(p) && p > 0 ? p : undefined
  } catch {
    return undefined
  }
}

function createWindow(): void {
  setStartupTimestamp('windowCreatedAt')
  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    show: false,
    autoHideMenuBar: true,
    titleBarStyle: 'hidden',
    trafficLightPosition: { x: 16, y: 16 },
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      sandbox: false,
    },
  })

  mainWindow.on('ready-to-show', () => {
    setStartupTimestamp('readyToShowAt')
    mainWindow!.show()
  })

  mainWindow.webContents.once('did-finish-load', () => {
    setStartupTimestamp('rendererDidFinishLoadAt')
  })

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url)
    return { action: 'deny' }
  })

  // Custom zoom shortcuts to ensure they work on all keyboard layouts
  mainWindow.webContents.on('before-input-event', (event, input) => {
    if (input.type !== 'keyDown') return
    const hasMeta = input.meta || input.control
    if (!hasMeta) return

    // Command/Ctrl + - : Zoom out
    if (input.key === '-' || input.code === 'Minus' || input.code === 'NumpadSubtract') {
      const current = mainWindow!.webContents.getZoomLevel()
      mainWindow!.webContents.setZoomLevel(Math.max(current - 0.5, -3))
      event.preventDefault()
    }
    // Command/Ctrl + =/+ : Zoom in
    else if (
      input.key === '=' ||
      input.key === '+' ||
      input.code === 'Equal' ||
      input.code === 'NumpadAdd'
    ) {
      const current = mainWindow!.webContents.getZoomLevel()
      mainWindow!.webContents.setZoomLevel(Math.min(current + 0.5, 3))
      event.preventDefault()
    }
    // Command/Ctrl + 0 : Reset zoom
    else if (input.key === '0' || input.code === 'Digit0' || input.code === 'Numpad0') {
      mainWindow!.webContents.setZoomLevel(0)
      event.preventDefault()
    }
  })

  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    setStartupTimestamp('rendererLoadStartedAt')
    mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL'])
  } else {
    setStartupTimestamp('rendererLoadStartedAt')
    mainWindow.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

app.whenReady().then(() => {
  setStartupTimestamp('whenReadyAt')
  electronApp.setAppUserModelId('com.droi.app')
  app.on('browser-window-created', (_, window) => {
    optimizer.watchWindowShortcuts(window, { zoom: true })
  })

  protocol.handle('local-file', (request) => {
    const filePath = decodeURIComponent(request.url.replace('local-file://', ''))
    return net.fetch(pathToFileURL(filePath).toString())
  })

  ipcMain.handle('startup:getMetrics', async () => getStartupMetricsSnapshot())
  ipcMain.on('startup:mark', (_event, payload: { name?: string; ts?: number }) => {
    const name = typeof payload?.name === 'string' ? payload.name : ''
    const ts =
      typeof payload?.ts === 'number' && Number.isFinite(payload.ts) ? payload.ts : Date.now()
    setRendererStartupMark(name, ts)
  })

  createWindow()
  const baseDir = (process.env['DROID_APP_DATA_DIR'] || '').trim() || app.getPath('userData')
  const diagnostics = new LocalDiagnostics({ baseDir })
  ipcCtl = registerIpcHandlers({
    getMainWindow: () => mainWindow,
    baseDir,
    diagnostics,
  })

  registerUpdaterIpc({ getMainWindow: () => mainWindow })

  const webEnabled = readBool('DROID_WEB_ENABLED', true)
  if (webEnabled) {
    const appStateStore = createAppStateStore({ baseDir })
    void appStateStore
      .load()
      .then((state) => {
        const lanEnabled = (state as any)?.lanAccessEnabled === true
        const envHost = (process.env['DROID_APP_API_HOST'] || '').trim()
        const host = envHost || (lanEnabled ? '0.0.0.0' : '127.0.0.1')
        const port = Number(process.env['DROID_APP_API_PORT'] || 3001)
        const webRootDir = join(__dirname, '../renderer')
        const pairingWebPort = is.dev
          ? parsePortFromUrl(process.env['ELECTRON_RENDERER_URL'])
          : undefined

        return startApiServer({
          host,
          port,
          baseDir,
          webRootDir,
          pairingWebPort,
          diagnostics,
          appVersion: app.getVersion(),
        }).then((started) => {
          apiCtl = { close: started.close }
          // eslint-disable-next-line no-console
          console.log(`Droid API server running at http://${started.host}:${started.port}`)
          // eslint-disable-next-line no-console
          console.log(`Web UI root: ${webRootDir}`)
        })
      })
      .catch((err) => {
        // eslint-disable-next-line no-console
        console.error('Failed to start Droid API server', err)
      })
  }

  app.on('before-quit', () => {
    ipcCtl?.cancelActiveRun()
    void ipcCtl?.close?.().catch(() => {})
    void apiCtl?.close().catch(() => {})
  })
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  ipcCtl?.cancelActiveRun()
  void ipcCtl?.close?.().catch(() => {})
  if (process.platform !== 'darwin') app.quit()
})
