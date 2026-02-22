import { Hono } from 'hono'
import { createApiRoutes } from './routes/apiRoutes.ts'
import { createMobileRoutes } from './routes/mobileRoutes.ts'
import { registerStaticRoutes } from './routes/staticRoutes.ts'
import type { HonoAppDeps, ServerEnv } from './types.ts'
import { applyCorsHeaders } from '../utils/http.ts'

export function createHonoApp(deps: HonoAppDeps) {
  const app = new Hono<ServerEnv>()

  app.use('*', async (c, next) => {
    c.set('deps', deps)
    c.set('appVersion', deps.opts.appVersion || 'N/A')

    if (c.req.method === 'OPTIONS') {
      applyCorsHeaders(c)
      return c.body(null, 204)
    }

    await next()
    applyCorsHeaders(c)
  })

  app.route('/mobile', createMobileRoutes())
  app.route('/api', createApiRoutes())
  registerStaticRoutes(app, deps.opts.webRootDir)

  app.notFound((c) => c.json({ error: 'Not Found' }, 404))

  app.onError((err, c) => {
    // eslint-disable-next-line no-console
    console.error('Unhandled API error', err)
    return c.json({ error: 'Internal Server Error' }, 500)
  })

  return app
}
