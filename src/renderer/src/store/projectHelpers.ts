import type { Project, SessionMeta } from '@/types'

export function getRepoKey(meta: Pick<SessionMeta, 'repoRoot' | 'projectDir'>): string {
  return String(meta.repoRoot || meta.projectDir || '').trim()
}

export function getTitleFromPrompt(prompt: string): string {
  const trimmed = String(prompt || '').trim() || 'Untitled'
  return trimmed.slice(0, 40) + (trimmed.length > 40 ? '...' : '')
}

export function upsertSessionMeta(prev: Project[], meta: SessionMeta): Project[] {
  const repoKey = getRepoKey(meta)
  if (!repoKey) return prev
  const cleaned = prev.map((p) => ({ ...p, sessions: p.sessions.filter((s) => s.id !== meta.id) }))
  const target = cleaned.find((p) => p.dir === repoKey)
  if (target) {
    target.sessions.push(meta)
    target.sessions.sort((a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt))
    return cleaned
  }
  const name = repoKey.split(/[\\/]/).pop() || repoKey
  return [...cleaned, { dir: repoKey, name, sessions: [meta] }]
}

export function updateSessionTitle(prev: Project[], sessionId: string, title: string): Project[] {
  let changed = false
  const next = prev.map((p) => {
    let sessionsChanged = false
    const sessions = p.sessions.map((s) => {
      if (s.id !== sessionId) return s
      if (s.title === title) return s
      changed = true
      sessionsChanged = true
      return { ...s, title }
    })
    return sessionsChanged ? { ...p, sessions } : p
  })
  return changed ? next : prev
}

export function replaceSessionIdInProjects(
  prev: Project[],
  oldId: string,
  nextMeta: SessionMeta,
): Project[] {
  if (!oldId || !nextMeta?.id || oldId === nextMeta.id) return upsertSessionMeta(prev, nextMeta)
  const cleaned = prev.map((p) => ({
    ...p,
    sessions: p.sessions.filter((s) => s.id !== nextMeta.id),
  }))
  let replaced = false
  const next = cleaned.map((p) => {
    const idx = p.sessions.findIndex((s) => s.id === oldId)
    if (idx === -1) return p
    replaced = true
    const sessions = p.sessions.slice()
    sessions[idx] = nextMeta
    return { ...p, sessions }
  })
  return replaced ? next : upsertSessionMeta(cleaned, nextMeta)
}
