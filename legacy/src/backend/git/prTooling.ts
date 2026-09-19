export type PrTool = 'gh' | 'flow'

export function parseGitRemoteHost(remoteUrl: string): string | null {
  const raw = String(remoteUrl || '').trim()
  if (!raw) return null

  // ssh url form: ssh://git@github.com/org/repo.git
  if (raw.startsWith('ssh://')) {
    try {
      const u = new URL(raw)
      return u.hostname ? u.hostname.toLowerCase() : null
    } catch {
      return null
    }
  }

  // scp-like ssh form: git@github.com:org/repo.git
  const scpLike = raw.match(/^[^@]+@([^:/]+)[:/].+$/)
  if (scpLike?.[1]) return scpLike[1].toLowerCase()

  // https form: https://github.com/org/repo.git
  try {
    const u = new URL(raw)
    return u.hostname ? u.hostname.toLowerCase() : null
  } catch {
    return null
  }
}

export function recommendPrTool(params: {
  originHost: string | null
  hasGh: boolean
  hasFlow: boolean
}): { prTool: PrTool | null; disabledReason?: string } {
  const host = params.originHost ? params.originHost.toLowerCase() : null
  const isGithub = host ? host === 'github.com' || host.endsWith('.github.com') : false

  if (isGithub) {
    if (params.hasGh) return { prTool: 'gh' }
    return {
      prTool: null,
      disabledReason: 'GitHub remote detected — install GitHub CLI (gh) to create PRs.',
    }
  }

  if (params.hasFlow) return { prTool: 'flow' }

  if (!host && params.hasGh) return { prTool: 'gh' }

  return {
    prTool: null,
    disabledReason:
      'PR creation requires `flow` (flow-cli). Install `flow` to enable PR creation for this remote.',
  }
}
