// The name a phone shows for this computer: macOS's Computer Name ("Clive's
// MacBook Pro") rather than the network host name, when it can be read.
import { execFileSync } from 'node:child_process'
import { hostname } from 'node:os'

export function computerName(): string {
  if (process.platform === 'darwin') {
    try {
      const name = execFileSync('/usr/sbin/scutil', ['--get', 'ComputerName'], {
        encoding: 'utf8',
        timeout: 2_000,
      }).trim()
      if (name) return name
    } catch {
      // Fall back to the host name.
    }
  }
  return hostname().replace(/\.local$/, '')
}
