// Droi's model export (droidhook.ExportModels). Droi runs `droid --version`
// once for each droid build with this file preloaded through BUN_OPTIONS; it
// writes what droid's own model registry says of each model: its credit
// multipliers, promotions and billing pool. The Daemon's droid never loads
// it.
import fs from 'fs'

const out = process.env.DROI_MODELS_OUT
const main = typeof Bun !== 'undefined' ? String(Bun.main ?? '') : ''
const isDroid = /^(\/\$bunfs\/|[A-Za-z]:[\\/]~BUN[\\/])/.test(main)
if (out && isDroid) {
  delete process.env.DROI_MODELS_OUT
  const result = { models: [] }
  const num = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : undefined)
  const time = (v) =>
    v instanceof Date && Number.isFinite(v.getTime()) ? v.toISOString() : undefined
  const isModel = (m) =>
    m &&
    typeof m === 'object' &&
    typeof m.id === 'string' &&
    num(m.cost?.tokenMultiplier) !== undefined
  try {
    // The bundle's modules sit next to droid's entry point; their names
    // change with every build, so the registry is found by its shape.
    const dir = main.replace(/[\\/][^\\/]*$/, '')
    let best = []
    for (const f of fs.readdirSync(dir)) {
      if (!f.endsWith('.js')) continue
      let src
      try {
        src = fs.readFileSync(dir + '/' + f, 'utf8')
      } catch {
        continue
      }
      // The CLI's output multipliers by provider, for a model whose cost
      // leaves its own out; the module keeps them to itself.
      const outputs = src.match(
        /\{(anthropic:[\d.]+(?:,[a-z]+:[\d.]+)+)\},[\w$]+=([\d.]+);function/,
      )
      if (outputs && !result.outputByProvider) {
        result.outputByProvider = Object.fromEntries(
          outputs[1].split(',').map((p) => [p.split(':')[0], Number(p.split(':')[1])]),
        )
        result.outputDefault = Number(outputs[2])
      }
      if (!src.includes('cost:{tokenMultiplier:')) continue
      const mod = await import(dir + '/' + f)
      for (const v of Object.values(mod)) {
        if (!v || typeof v !== 'object' || Array.isArray(v)) continue
        const models = Object.values(v).filter(isModel)
        if (models.length > best.length) best = models
      }
    }
    result.models = best.map((m) => ({
      id: m.id,
      name: typeof m.name === 'string' ? m.name : undefined,
      provider: typeof m.provider === 'string' ? m.provider : undefined,
      billingPool: typeof m.billingPool === 'string' ? m.billingPool : undefined,
      tokenMultiplier: m.cost.tokenMultiplier,
      outputTokenMultiplier: num(m.cost.outputTokenMultiplier),
      cacheReadTokenMultiplier: num(m.cost.cacheReadTokenMultiplier),
      promotions: (Array.isArray(m.cost.promotions) ? m.cost.promotions : [])
        .filter((p) => num(p?.discount) !== undefined)
        .map((p) => ({
          discount: p.discount,
          startsAt: time(p.startsAt),
          expiresAt: time(p.expiresAt),
        })),
    }))
  } catch (e) {
    result.error = String(e?.stack ?? e)
  }
  fs.writeFileSync(out, JSON.stringify(result))
}
