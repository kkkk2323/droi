import React, { useMemo, useState } from 'react'
import { useDebugTrace, useDiagnosticsDir, useActiveSessionId, useActions } from '@/store'

export function DebugTracePanel(): React.JSX.Element | null {
  const debugTrace = useDebugTrace()
  const diagnosticsDir = useDiagnosticsDir()
  const activeSessionId = useActiveSessionId()
  const { clearDebugTrace, exportDiagnostics, openPath } = useActions()
  const [copied, setCopied] = useState(false)
  const [exportedPath, setExportedPath] = useState('')
  const [exporting, setExporting] = useState(false)

  const enabled = (import.meta as any)?.env?.DEV || debugTrace.length > 0
  const text = useMemo(() => debugTrace.join('\n'), [debugTrace])

  if (!enabled) return null

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text || '')
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

  return (
    <details className="mx-auto w-full max-w-3xl px-6 pb-2 text-xs text-muted-foreground">
      <summary className="cursor-pointer select-none py-1">
        Debug trace {debugTrace.length ? `(${debugTrace.length} lines)` : ''}
      </summary>
      <div className="mt-1 flex items-center gap-2">
        <button
          type="button"
          className="rounded border px-2 py-1 text-xs hover:bg-muted"
          onClick={onCopy}
          disabled={!debugTrace.length}
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
        <button
          type="button"
          className="rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-60"
          onClick={onExport}
          disabled={exporting}
        >
          {exporting ? 'Exporting...' : 'Export zip'}
        </button>
        <button
          type="button"
          className="rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-60"
          onClick={() => void openPath(diagnosticsDir)}
          disabled={!diagnosticsDir}
        >
          Open logs
        </button>
        <button
          type="button"
          className="rounded border px-2 py-1 text-xs hover:bg-muted"
          onClick={clearDebugTrace}
          disabled={!debugTrace.length}
        >
          Clear
        </button>
        <span className="opacity-70">Includes raw protocol + prompts; redact before sharing.</span>
      </div>
      {exportedPath && (
        <div className="mt-2 truncate font-mono opacity-80">Export: {exportedPath}</div>
      )}
      <pre className="mt-2 max-h-48 overflow-auto rounded border bg-background p-2 text-[11px] leading-4 text-foreground/80">
        {text || '(empty)'}
      </pre>
    </details>
  )
}
