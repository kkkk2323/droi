const ADJECTIVES = [
  'brave',
  'calm',
  'clever',
  'crisp',
  'eager',
  'gentle',
  'happy',
  'keen',
  'lucky',
  'mellow',
  'noble',
  'quick',
  'quiet',
  'sharp',
  'solid',
  'sunny',
  'tidy',
  'warm',
  'witty',
  'zesty',
]

const NOUNS = [
  'otter',
  'falcon',
  'panda',
  'tiger',
  'wolf',
  'eagle',
  'whale',
  'fox',
  'koala',
  'badger',
  'shark',
  'yak',
  'manta',
  'squid',
  'rook',
  'lark',
  'cactus',
  'maple',
  'comet',
  'ember',
]

function randomInt(maxExclusive: number): number {
  return Math.floor(Math.random() * maxExclusive)
}

function randomBase36(len: number): string {
  let out = ''
  while (out.length < len) out += Math.random().toString(36).slice(2)
  return out.slice(0, len)
}

export function sanitizeWorktreePrefix(prefix: string): string {
  const raw = String(prefix || '')
    .trim()
    .replace(/^\/+|\/+$/g, '')
  const cleaned = raw
    .replace(/\s+/g, '-')
    .replace(/[^A-Za-z0-9._-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-+|-+$/g, '')
  return cleaned
}

export function generateWorktreeBranch(prefix: string): string {
  const p = sanitizeWorktreePrefix(prefix) || 'droi'
  const adj = ADJECTIVES[randomInt(ADJECTIVES.length)] || 'new'
  const noun = NOUNS[randomInt(NOUNS.length)] || 'session'
  const suffix = randomBase36(4)
  return `${p}/${adj}-${noun}-${suffix}`
}

export function defaultSessionTitleFromBranch(branch: string): string {
  const b = String(branch || '').trim()
  if (!b) return 'Untitled'
  const last = b.split('/').pop() || b
  return last.slice(0, 40) + (last.length > 40 ? '...' : '')
}
