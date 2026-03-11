export type SessionKind = 'normal' | 'mission'

export type SessionInteractionMode = 'spec' | 'auto' | 'agi'

export type SessionAutonomyLevel = 'off' | 'low' | 'medium' | 'high'

export type DecompSessionType = 'orchestrator'

export interface SessionProtocolFields {
  isMission?: boolean
  sessionKind?: SessionKind
  interactionMode?: SessionInteractionMode
  autonomyLevel?: SessionAutonomyLevel
  decompSessionType?: DecompSessionType
}

export function interactionModeFromAutoLevel(autoLevel: unknown): SessionInteractionMode {
  const value = typeof autoLevel === 'string' ? autoLevel : 'default'
  return value === 'default' ? 'spec' : 'auto'
}

export function autonomyLevelFromAutoLevel(autoLevel: unknown): SessionAutonomyLevel {
  const value = typeof autoLevel === 'string' ? autoLevel : 'default'
  if (value === 'low') return 'low'
  if (value === 'medium') return 'medium'
  if (value === 'high') return 'high'
  return 'off'
}

export function isMissionSessionProtocol(fields?: SessionProtocolFields | null): boolean {
  if (!fields) return false
  return (
    fields.isMission === true ||
    fields.sessionKind === 'mission' ||
    fields.interactionMode === 'agi' ||
    fields.decompSessionType === 'orchestrator'
  )
}

export function resolveSessionProtocolFields(opts: {
  explicit?: SessionProtocolFields | null
  existing?: SessionProtocolFields | null
  autoLevel?: unknown
}): SessionProtocolFields {
  const explicit = opts.explicit || undefined
  const existing = opts.existing || undefined
  const mission = isMissionSessionProtocol(explicit) || isMissionSessionProtocol(existing)

  if (mission) {
    const existingAutonomy = existing?.autonomyLevel
    const explicitAutonomy = explicit?.autonomyLevel
    const derivedAutonomy =
      typeof opts.autoLevel === 'string' && opts.autoLevel !== 'default'
        ? autonomyLevelFromAutoLevel(opts.autoLevel)
        : undefined
    return {
      isMission: true,
      sessionKind: 'mission',
      interactionMode: 'agi',
      autonomyLevel: existingAutonomy || explicitAutonomy || derivedAutonomy,
      decompSessionType: 'orchestrator',
    }
  }

  const hasDerivedAutoLevel = typeof opts.autoLevel === 'string'
  const derivedInteractionMode = hasDerivedAutoLevel
    ? interactionModeFromAutoLevel(opts.autoLevel)
    : undefined
  const derivedAutonomyLevel = hasDerivedAutoLevel
    ? autonomyLevelFromAutoLevel(opts.autoLevel)
    : undefined

  return {
    isMission: false,
    sessionKind: 'normal',
    interactionMode:
      derivedInteractionMode ||
      explicit?.interactionMode ||
      existing?.interactionMode ||
      interactionModeFromAutoLevel(opts.autoLevel),
    autonomyLevel:
      derivedAutonomyLevel ||
      explicit?.autonomyLevel ||
      existing?.autonomyLevel ||
      autonomyLevelFromAutoLevel(opts.autoLevel),
  }
}
