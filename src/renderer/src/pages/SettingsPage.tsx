import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ChevronRightIcon } from 'lucide-react'
import { useNavigate } from '@tanstack/react-router'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { ModelSelect } from '@/components/ModelSelect'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getDroidClient } from '@/droidClient'
import { DEFAULT_MODEL } from '@/state/appReducer'
import { getModelReasoningLevels, getModelDefaultReasoning } from '@/types'
import {
  useCustomModels,
  useCommitMessageModelId,
  useCommitMessageReasoningEffort,
  useLanAccessEnabled,
  useTelemetryEnabled,
  useMissionModelSettings,
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

function ModelOverrideRow({
  title,
  description,
  follow,
  onFollowChange,
  value,
  onChange,
  customModels,
}: {
  title: string
  description: string
  follow: boolean
  onFollowChange: (checked: boolean) => void
  value: string
  onChange: (value: string) => void
  customModels: import('@/types').CustomModelDef[]
}) {
  return (
    <div className="space-y-2 rounded-md border border-border p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-sm font-medium">{title}</div>
          <div className="text-xs text-muted-foreground">{description}</div>
        </div>
        <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
          <Switch checked={follow} onCheckedChange={onFollowChange} />
          Follow orchestrator
        </label>
      </div>
      <ModelSelect
        value={value}
        onChange={onChange}
        customModels={customModels}
        disabled={follow}
        className="w-full"
      />
    </div>
  )
}

export function SettingsPage() {
  const navigate = useNavigate()
  const customModels = useCustomModels()
  const commitMessageModelId = useCommitMessageModelId()
  const commitMessageReasoningEffort = useCommitMessageReasoningEffort()
  const lanAccessEnabled = useLanAccessEnabled()
  const telemetryEnabled = useTelemetryEnabled()
  const missionModelSettings = useMissionModelSettings()
  const appVersion = useAppVersion()
  const droidVersion = useDroidVersion()
  const {
    setCommitMessageModelId,
    setCommitMessageReasoningEffort,
    setLanAccessEnabled,
    setTelemetryEnabled,
    setMissionModelSettings,
  } = useActions()

  const [update, setUpdate] = useState<UpdateState>({ step: 'idle' })
  const [missionModelsOpen, setMissionModelsOpen] = useState(false)
  const [followWorkerModel, setFollowWorkerModel] = useState(true)
  const [followValidatorModel, setFollowValidatorModel] = useState(true)
  const [workerModel, setWorkerModel] = useState(DEFAULT_MODEL)
  const [validatorModel, setValidatorModel] = useState(DEFAULT_MODEL)
  const unsubRef = useRef<(() => void) | null>(null)

  const orchestratorModel = missionModelSettings.orchestratorModel || DEFAULT_MODEL

  useEffect(() => {
    return () => {
      unsubRef.current?.()
    }
  }, [])

  useEffect(() => {
    const nextWorkerModel = missionModelSettings.workerModel || orchestratorModel
    const nextValidatorModel = missionModelSettings.validationWorkerModel || orchestratorModel
    const nextFollowWorkerModel =
      !missionModelSettings.workerModel || missionModelSettings.workerModel === orchestratorModel
    const nextFollowValidatorModel =
      !missionModelSettings.validationWorkerModel ||
      missionModelSettings.validationWorkerModel === orchestratorModel

    setWorkerModel(nextWorkerModel)
    setValidatorModel(nextValidatorModel)
    setFollowWorkerModel(nextFollowWorkerModel)
    setFollowValidatorModel(nextFollowValidatorModel)
    if (!nextFollowWorkerModel || !nextFollowValidatorModel) {
      setMissionModelsOpen(true)
    }
  }, [
    missionModelSettings.workerModel,
    missionModelSettings.validationWorkerModel,
    orchestratorModel,
  ])

  const persistMissionModels = useCallback(
    (next: {
      orchestratorModel?: string
      workerModel?: string
      validationWorkerModel?: string
      followWorkerModel?: boolean
      followValidatorModel?: boolean
    }) => {
      const nextOrchestratorModel = next.orchestratorModel || orchestratorModel
      const nextFollowWorkerModel = next.followWorkerModel ?? followWorkerModel
      const nextFollowValidatorModel = next.followValidatorModel ?? followValidatorModel
      const nextWorkerModel = next.workerModel || workerModel || nextOrchestratorModel
      const nextValidationWorkerModel =
        next.validationWorkerModel || validatorModel || nextOrchestratorModel

      return setMissionModelSettings({
        orchestratorModel: nextOrchestratorModel,
        workerModel: nextFollowWorkerModel ? nextOrchestratorModel : nextWorkerModel,
        validationWorkerModel: nextFollowValidatorModel
          ? nextOrchestratorModel
          : nextValidationWorkerModel,
      })
    },
    [
      orchestratorModel,
      followWorkerModel,
      followValidatorModel,
      workerModel,
      validatorModel,
      setMissionModelSettings,
    ],
  )

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
              Manage your Factory API keys. Droi prioritizes the earliest expiry, then the lowest
              usage, and rotates at 98%.
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

          <div className="flex items-center gap-2">
            <ModelSelect
              value={commitMessageModelId}
              onChange={setCommitMessageModelId}
              customModels={customModels}
              className="flex-1"
            />
            {(() => {
              const levels = getModelReasoningLevels(commitMessageModelId)
              if (!levels) return null
              const displayValue =
                commitMessageReasoningEffort ||
                getModelDefaultReasoning(commitMessageModelId) ||
                levels[0]
              return (
                <div className="flex shrink-0 items-center gap-2">
                  <span className="text-sm text-muted-foreground">Reasoning</span>
                  <Select
                    value={displayValue}
                    onValueChange={(v) => v && setCommitMessageReasoningEffort(v)}
                  >
                    <SelectTrigger className="w-auto">
                      <SelectValue>{displayValue}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {levels.map((l) => (
                        <SelectItem key={l} value={l}>
                          {l}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )
            })()}
          </div>
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Mission Models</h2>
            <p className="text-xs text-muted-foreground">
              The orchestrator model is the real runtime model for Mission chat sessions. Worker and
              validator models default to following it.
            </p>
          </div>

          <div className="space-y-2">
            <div className="space-y-1">
              <span className="text-sm text-muted-foreground">Orchestrator model</span>
              <ModelSelect
                value={orchestratorModel}
                onChange={(value) => {
                  const nextModel = value || DEFAULT_MODEL
                  if (followWorkerModel) setWorkerModel(nextModel)
                  if (followValidatorModel) setValidatorModel(nextModel)
                  void persistMissionModels({
                    orchestratorModel: nextModel,
                    workerModel: followWorkerModel ? nextModel : workerModel,
                    validationWorkerModel: followValidatorModel ? nextModel : validatorModel,
                  })
                }}
                customModels={customModels}
                className="w-full"
              />
              <p className="text-xs text-muted-foreground">
                Used by the Mission orchestrator session when you send messages from Mission chat.
              </p>
            </div>

            <Collapsible open={missionModelsOpen} onOpenChange={setMissionModelsOpen}>
              <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-accent/40">
                <div>
                  <div className="font-medium">Advanced overrides</div>
                  <div className="text-xs text-muted-foreground">
                    Use different models for workers and validators.
                  </div>
                </div>
                <ChevronRightIcon
                  className={`size-4 transition-transform ${missionModelsOpen ? 'rotate-90' : ''}`}
                />
              </CollapsibleTrigger>
              <CollapsibleContent className="space-y-4 px-1 pt-3">
                <ModelOverrideRow
                  title="Worker model"
                  description="Runs implementation tasks for each feature worker."
                  follow={followWorkerModel}
                  onFollowChange={(checked) => {
                    setFollowWorkerModel(checked)
                    if (checked) {
                      setWorkerModel(orchestratorModel)
                      void persistMissionModels({
                        followWorkerModel: true,
                        workerModel: orchestratorModel,
                      })
                    }
                  }}
                  value={followWorkerModel ? orchestratorModel : workerModel}
                  onChange={(value) => {
                    setWorkerModel(value)
                    if (!followWorkerModel) {
                      void persistMissionModels({
                        followWorkerModel: false,
                        workerModel: value,
                      })
                    }
                  }}
                  customModels={customModels}
                />
                <ModelOverrideRow
                  title="Validator model"
                  description="Runs validation workers such as scrutiny and user testing."
                  follow={followValidatorModel}
                  onFollowChange={(checked) => {
                    setFollowValidatorModel(checked)
                    if (checked) {
                      setValidatorModel(orchestratorModel)
                      void persistMissionModels({
                        followValidatorModel: true,
                        validationWorkerModel: orchestratorModel,
                      })
                    }
                  }}
                  value={followValidatorModel ? orchestratorModel : validatorModel}
                  onChange={(value) => {
                    setValidatorModel(value)
                    if (!followValidatorModel) {
                      void persistMissionModels({
                        followValidatorModel: false,
                        validationWorkerModel: value,
                      })
                    }
                  }}
                  customModels={customModels}
                />
              </CollapsibleContent>
            </Collapsible>
          </div>
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
            <h2 className="text-sm font-medium">Usage Analytics</h2>
            <p className="text-xs text-muted-foreground">
              Send anonymous usage data to help improve Droi. No personal information, chat content,
              code, or file paths are ever collected.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
            <Switch checked={telemetryEnabled} onCheckedChange={setTelemetryEnabled} />
            Enable anonymous usage analytics
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
                <span className="text-sm text-muted-foreground">
                  You are on the latest version.
                </span>
                <Button variant="outline" size="sm" onClick={handleCheck}>
                  Check Again
                </Button>
              </div>
            )}
            {update.step === 'available' && (
              <div className="flex items-center gap-3">
                <span className="text-sm">v{update.version} available</span>
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
