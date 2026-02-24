import { ipcMain, type BrowserWindow } from 'electron'
import { AsarUpdater } from './asarUpdater'
import type { DownloadProgress } from './asarUpdater'

export function registerUpdaterIpc(opts: { getMainWindow: () => BrowserWindow | null }) {
  const updater = new AsarUpdater()

  ipcMain.handle('updater:check', async () => {
    return updater.check()
  })

  ipcMain.handle('updater:install', async () => {
    const progressHandler = (progress: DownloadProgress) => {
      const win = opts.getMainWindow()
      if (win && !win.isDestroyed()) {
        win.webContents.send('updater:progress', progress)
      }
    }
    updater.on('progress', progressHandler)
    try {
      await updater.downloadAndInstall()
    } finally {
      updater.removeListener('progress', progressHandler)
    }
  })

  ipcMain.handle('updater:relaunch', () => {
    updater.relaunch()
  })
}
