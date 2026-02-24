import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { ModelSelect } from '@/components/ModelSelect'
import { getDroidClient } from '@/droidClient'
import {
  useCustomModels,
  useCommitMessageModelId,
  useLanAccessEnabled,
  useActions,
  useAppVersion,
  useDroidVersion,
} from '@/store'

type UpdateState =
  | { step: 'idle' }
  | { step: 'checking' }
  | { step: 'not-available' }
  | { step: 'available'; version: string }
  | { step: 'downloading'; percent: number }
  | { step: 'ready' }
  | { step: 'error'; message: string }

export function SettingsPage() {
  const navigate = useNavigate()
  const customModels = useCustomModels()
  const commitMessageModelId = useCommitMessageModelId()
  const lanAccessEnabled = useLanAccessEnabled()
  const appVersion = useAppVersion()
  const droidVersion = useDroidVersion()
  const { setCommitMessageModelId, setLanAccessEnabled } = useActions()

  const [update, setUpdate] = useState<UpdateState>({ step: 'idle' })
  const unsubRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    return () => {
      unsubRef.current?.()
    }
  }, [])

  const handleCheck = useCallback(async () => {
    setUpdate({ step: 'checking' })
    try {
      const droid = getDroidClient()
      const result = await droid.checkForUpdate()
      if (result.available && result.version) {
        setUpdate({ step: 'available', version: result.version })
      } else {
        setUpdate({ step: 'not-available' })
      }
    } catch (err) {
      const raw = err instanceof Error ? err.message : String(err)
      const cleaned = raw.replace(/^Error invoking remote method '[^']+': Error:\s*/i, '')
      setUpdate({ step: 'error', message: cleaned || 'Could not reach update server' })
    }
  }, [])

  const handleInstall = useCallback(async () => {
    setUpdate({ step: 'downloading', percent: 0 })
    try {
      const droid = getDroidClient()
      unsubRef.current?.()
      unsubRef.current = droid.onUpdateProgress((progress) => {
        setUpdate({ step: 'downloading', percent: Math.round(progress.percent * 100) })
      })
      await droid.installUpdate()
      unsubRef.current?.()
      unsubRef.current = null
      setUpdate({ step: 'ready' })
    } catch (err) {
      unsubRef.current?.()
      unsubRef.current = null
      const raw = err instanceof Error ? err.message : String(err)
      const cleaned = raw.replace(/^Error invoking remote method '[^']+': Error:\s*/i, '')
      setUpdate({ step: 'error', message: cleaned || 'Update failed' })
    }
  }, [])

  const handleRelaunch = useCallback(() => {
    const droid = getDroidClient()
    void droid.relaunchApp()
  }, [])

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="mx-auto w-full max-w-2xl space-y-8 p-8">
        <div>
          <h1 className="text-xl font-semibold">Settings</h1>
          <p className="mt-1 text-sm text-muted-foreground">Configure your Droi environment.</p>
        </div>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">API Keys</h2>
            <p className="text-xs text-muted-foreground">
              Manage your Factory API keys. Keys are automatically rotated based on expiry date.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => navigate({ to: '/settings/keys' })}>
            Manage Keys
          </Button>
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Commit</h2>
            <p className="text-xs text-muted-foreground">
              Base model used to generate commit messages (and PR title/body) when the Commit dialog
              input is empty.
            </p>
          </div>

          <ModelSelect
            value={commitMessageModelId}
            onChange={setCommitMessageModelId}
            customModels={customModels}
          />
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">LAN Access</h2>
            <p className="text-xs text-muted-foreground">
              Allow devices on the same network to access Droi. Requires restart to take effect.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
            <Switch checked={lanAccessEnabled} onCheckedChange={setLanAccessEnabled} />
            Enable LAN access
          </label>
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Updates</h2>
            <p className="text-xs text-muted-foreground">
              Check for new versions and update without re-downloading the full installer.
            </p>
          </div>
          <div className="flex items-center gap-3">
            {update.step === 'idle' && (
              <Button variant="outline" size="sm" onClick={handleCheck}>
                Check for Updates
              </Button>
            )}
            {update.step === 'checking' && (
              <span className="text-sm text-muted-foreground">Checking...</span>
            )}
            {update.step === 'not-available' && (
              <div className="flex items-center gap-3">
                <span className="text-sm text-muted-foreground">You are on the latest version.</span>
                <Button variant="outline" size="sm" onClick={handleCheck}>
                  Check Again
                </Button>
              </div>
            )}
            {update.step === 'available' && (
              <div className="flex items-center gap-3">
                <span className="text-sm">
                  v{update.version} available
                </span>
                <Button variant="outline" size="sm" onClick={handleInstall}>
                  Download & Install
                </Button>
              </div>
            )}
            {update.step === 'downloading' && (
              <div className="flex items-center gap-3">
                <div className="h-2 w-48 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-foreground transition-all"
                    style={{ width: `${update.percent}%` }}
                  />
                </div>
                <span className="text-sm text-muted-foreground">{update.percent}%</span>
              </div>
            )}
            {update.step === 'ready' && (
              <div className="flex items-center gap-3">
                <span className="text-sm">Update installed.</span>
                <Button variant="outline" size="sm" onClick={handleRelaunch}>
                  Restart Now
                </Button>
              </div>
            )}
            {update.step === 'error' && (
              <div className="flex items-center gap-3">
                <span className="text-sm text-destructive">{update.message}</span>
                <Button variant="outline" size="sm" onClick={handleCheck}>
                  Retry
                </Button>
              </div>
            )}
          </div>
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">About</h2>
          </div>
          <div className="space-y-1 text-sm text-muted-foreground">
            <p>
              Droi <span className="font-mono">v{appVersion}</span>
            </p>
            <p>
              Droid CLI <span className="font-mono">v{droidVersion}</span>
            </p>
          </div>
        </section>
      </div>
    </div>
  )
}
