const REQUIRED_DROID_HOOKS = [
  'onRpcNotification',
  'onRpcRequest',
  'onTurnEnd',
  'onStdout',
  'onStderr',
  'onError',
  'onSetupScriptEvent',
] as const

export type RequiredDroidHookName = (typeof REQUIRED_DROID_HOOKS)[number]

export function getMissingDroidHooks(client: unknown): RequiredDroidHookName[] {
  if (!client || typeof client !== 'object') return [...REQUIRED_DROID_HOOKS]
  const obj = client as Record<string, unknown>
  return REQUIRED_DROID_HOOKS.filter((name) => typeof obj[name] !== 'function')
}

export function buildHookMismatchMessage(missingHooks: string[]): string {
  return `Droid preload API mismatch: missing hooks [${missingHooks.join(', ')}]. Fully restart the Electron app/dev process so updated preload is loaded.`
}
