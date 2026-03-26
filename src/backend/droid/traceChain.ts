let traceChainEnabledOverride: boolean | undefined

function isEnabledLike(value: unknown): boolean {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  return normalized === '1' || normalized === 'true' || normalized === 'yes' || normalized === 'on'
}

export function setTraceChainEnabledOverride(enabled: boolean | undefined): void {
  traceChainEnabledOverride = typeof enabled === 'boolean' ? enabled : undefined
}

export function isTraceChainEnabled(): boolean {
  if (typeof traceChainEnabledOverride === 'boolean') return traceChainEnabledOverride
  if (isEnabledLike(process.env['DROID_TRACE_CHAIN'])) return true
  if (isEnabledLike(process.env['VITE_DROID_TRACE_CHAIN'])) return true
  return false
}
