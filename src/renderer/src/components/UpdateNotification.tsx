import { useEffect, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Download, RefreshCw, X } from 'lucide-react'
import { getDroidClient } from '@/droidClient'
import {
  useUpdateAvailable,
  useUpdateDownloading,
  useUpdateDownloadProgress,
  useUpdateReady,
  useActions,
} from '@/store'
import { isBrowserMode } from '@/droidClient'

export function UpdateNotification() {
  const updateAvailable = useUpdateAvailable()
  const updateDownloading = useUpdateDownloading()
  const updateDownloadProgress = useUpdateDownloadProgress()
  const updateReady = useUpdateReady()
  const { setUpdateDownloading, setUpdateDownloadProgress, setUpdateReady } = useActions()
  const unsubRef = useRef<(() => void) | null>(null)
  const [dismissed, setDismissed] = useState(false)

  // Skip in browser mode
  if (isBrowserMode()) {
    return null
  }

  const hasUpdate = updateAvailable && !dismissed
  const showDownloading = updateDownloading && !dismissed
  const showReady = updateReady && !dismissed

  const handleDownload = async () => {
    setUpdateDownloading(true)
    setUpdateDownloadProgress(0)
    try {
      const droid = getDroidClient()
      unsubRef.current?.()
      unsubRef.current = droid.onUpdateProgress((progress) => {
        setUpdateDownloadProgress(Math.round(progress.percent * 100))
      })
      await droid.installUpdate()
      unsubRef.current?.()
      unsubRef.current = null
      setUpdateDownloading(false)
      setUpdateReady(true)
    } catch (err) {
      unsubRef.current?.()
      unsubRef.current = null
      setUpdateDownloading(false)
      console.error('Update failed:', err)
    }
  }

  const handleRestart = () => {
    const droid = getDroidClient()
    void droid.relaunchApp()
  }

  const handleDismiss = () => {
    setDismissed(true)
  }

  useEffect(() => {
    return () => {
      unsubRef.current?.()
    }
  }, [])

  if (!hasUpdate && !showDownloading && !showReady) {
    return null
  }

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, y: 20, scale: 0.95 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 20, scale: 0.95 }}
        transition={{ duration: 0.2 }}
        className="fixed bottom-4 left-4 z-50 max-w-xs"
      >
        <div className="rounded-lg border bg-popover p-3 shadow-lg">
          <div className="flex items-start gap-3">
            <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/10">
              {showReady ? (
                <RefreshCw className="size-4 text-primary" />
              ) : (
                <Download className="size-4 text-primary" />
              )}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium">
                {showReady
                  ? 'Update ready'
                  : showDownloading
                    ? <>Downloading... <span className="tabular-nums">{updateDownloadProgress}%</span></>
                    : `v${updateAvailable?.version} available`}
              </p>
              <p className="text-xs text-muted-foreground mt-0.5">
                {showReady
                  ? 'Restart to complete update'
                  : showDownloading
                    ? 'Please wait...'
                    : 'Click to download and install'}
              </p>
              {showDownloading && (
                <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-primary transition-all"
                    style={{ width: `${updateDownloadProgress}%` }}
                  />
                </div>
              )}
              {!showDownloading && !showReady && (
                <button
                  onClick={handleDownload}
                  className="mt-2 text-xs font-medium text-primary hover:underline"
                >
                  Download update
                </button>
              )}
              {showReady && (
                <button
                  onClick={handleRestart}
                  className="mt-2 text-xs font-medium text-primary hover:underline"
                >
                  Restart now
                </button>
              )}
            </div>
            <button
              onClick={handleDismiss}
              className="shrink-0 rounded-md p-1 hover:bg-muted"
            >
              <X className="size-3.5 text-muted-foreground" />
            </button>
          </div>
        </div>
      </motion.div>
    </AnimatePresence>
  )
}
