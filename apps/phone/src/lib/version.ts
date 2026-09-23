// The Phone App carries the desktop version it was built from; a computer
// running another version still works, but may lack or change something.

export function versionMismatch(appVersion: string, computerVersion: string): boolean {
  const clean = (v: string) => v.trim().replace(/^v/, '')
  return (
    clean(appVersion) !== '' &&
    clean(computerVersion) !== '' &&
    clean(appVersion) !== clean(computerVersion)
  )
}
