import React, { useEffect, useState } from 'react'
import {
  useTraceChainEnabled, useShowDebugTrace, useLocalDiagnosticsEnabled,
  useLocalDiagnosticsRetentionDays, useLocalDiagnosticsMaxTotalMb,
  useDiagnosticsDir, useActiveSessionId, useActions,
} from '@/store'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export function DebugSettingsPage() {
  const traceChainEnabled = useTraceChainEnabled()
  const showDebugTrace = useShowDebugTrace()
  const localDiagnosticsEnabled = useLocalDiagnosticsEnabled()
  const localDiagnosticsRetentionDays = useLocalDiagnosticsRetentionDays()
  const localDiagnosticsMaxTotalMb = useLocalDiagnosticsMaxTotalMb()
  const diagnosticsDir = useDiagnosticsDir()
  const activeSessionId = useActiveSessionId()
  const {
    setTraceChainEnabled, setShowDebugTrace, setLocalDiagnosticsEnabled,
    setLocalDiagnosticsRetention, refreshDiagnosticsDir, exportDiagnostics, openPath,
  } = useActions()

  const [copied, setCopied] = useState(false)
  const [exportedPath, setExportedPath] = useState('')
  const [exporting, setExporting] = useState(false)
  const [retentionDaysDraft, setRetentionDaysDraft] = useState(String(localDiagnosticsRetentionDays))
  const [maxTotalMbDraft, setMaxTotalMbDraft] = useState(String(localDiagnosticsMaxTotalMb))

  useEffect(() => {
    void refreshDiagnosticsDir()
  }, [refreshDiagnosticsDir])

  useEffect(() => {
    setRetentionDaysDraft(String(localDiagnosticsRetentionDays))
  }, [localDiagnosticsRetentionDays])

  useEffect(() => {
    setMaxTotalMbDraft(String(localDiagnosticsMaxTotalMb))
  }, [localDiagnosticsMaxTotalMb])

  const onCopyDir = async () => {
    try {
      await navigator.clipboard.writeText(diagnosticsDir || '')
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // ignore
    }
  }

  const onExport = async () => {
    if (exporting) return
    setExporting(true)
    setExportedPath('')
    try {
      const res = await exportDiagnostics({ sessionId: activeSessionId })
      setExportedPath(String(res?.path || ''))
    } catch {
      setExportedPath('(failed)')
    } finally {
      setExporting(false)
    }
  }

  const commitRetention = () => {
    const days = Number(retentionDaysDraft)
    const mb = Number(maxTotalMbDraft)
    if (!Number.isFinite(days) || !Number.isFinite(mb)) return
    setLocalDiagnosticsRetention({ retentionDays: days, maxTotalMb: mb })
  }

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="mx-auto w-full max-w-2xl space-y-8 p-8">
        {/* Trace Chain Debug */}
        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Trace Chain Debug</h2>
            <p className="text-xs text-muted-foreground">
              Emit fingerprinted notification chain logs (`trace-chain`) across backend and renderer.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={traceChainEnabled}
              onCheckedChange={setTraceChainEnabled}
            />
            Enable trace-chain diagnostics
          </label>
        </section>

        <Separator />

        {/* Local Diagnostics */}
        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Local Diagnostics</h2>
            <p className="text-xs text-muted-foreground">
              Persist redacted diagnostics logs locally and export a zip bundle for intermittent issues.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={localDiagnosticsEnabled}
              onCheckedChange={setLocalDiagnosticsEnabled}
            />
            Enable local diagnostics logging (redacted)
          </label>

          <div className="rounded-lg border border-border bg-card/50 p-3 text-xs">
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0">
                <div className="font-medium">Diagnostics dir</div>
                <div className="mt-1 truncate font-mono opacity-80">{diagnosticsDir || '(unknown)'}</div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void refreshDiagnosticsDir()}
                >
                  Refresh
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={onCopyDir}
                  disabled={!diagnosticsDir}
                >
                  {copied ? 'Copied' : 'Copy'}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void openPath(diagnosticsDir)}
                  disabled={!diagnosticsDir}
                >
                  Open
                </Button>
              </div>
            </div>

            <div className="mt-3 grid grid-cols-2 gap-3">
              <label className="space-y-1">
                <div className="font-medium">Retention (days)</div>
                <Input
                  type="number"
                  min={1}
                  step={1}
                  className="font-mono"
                  value={retentionDaysDraft}
                  onChange={(e) => setRetentionDaysDraft(e.target.value)}
                  onBlur={commitRetention}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') commitRetention()
                  }}
                  disabled={!localDiagnosticsEnabled}
                />
              </label>
              <label className="space-y-1">
                <div className="font-medium">Max total (MB)</div>
                <Input
                  type="number"
                  min={1}
                  step={1}
                  className="font-mono"
                  value={maxTotalMbDraft}
                  onChange={(e) => setMaxTotalMbDraft(e.target.value)}
                  onBlur={commitRetention}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') commitRetention()
                  }}
                  disabled={!localDiagnosticsEnabled}
                />
              </label>
            </div>

            <div className="mt-3 flex items-center justify-between gap-2">
              <div className="min-w-0">
                <div className="font-medium">Export bundle</div>
                <div className="mt-1 truncate font-mono opacity-80">{exportedPath || ' '}</div>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="shrink-0"
                onClick={onExport}
                disabled={!localDiagnosticsEnabled || exporting}
              >
                {exporting ? 'Exporting...' : 'Export zip'}
              </Button>
            </div>
          </div>
        </section>

        <Separator />

        {/* Show Debug Trace in Chat */}
        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Show Debug Trace in Chat</h2>
            <p className="text-xs text-muted-foreground">
              Display the debug trace panel in the chat view for troubleshooting.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={showDebugTrace}
              onCheckedChange={setShowDebugTrace}
            />
            Show debug trace panel in ChatView
          </label>
        </section>
      </div>
    </div>
  )
}
