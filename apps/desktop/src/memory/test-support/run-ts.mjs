// Lets the tests run the Memory Server, the hook and small scripts as real
// processes straight from the TypeScript sources: Node strips the types, and
// this resolves the extensionless relative imports the bundler would.
// Usage: node --import ./run-ts.mjs <entry>.ts
import { registerHooks } from 'node:module'

registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context)
    } catch (error) {
      const relative = specifier.startsWith('./') || specifier.startsWith('../')
      if (error?.code === 'ERR_MODULE_NOT_FOUND' && relative && !/\.[cm]?[jt]s$/.test(specifier)) {
        return next(`${specifier}.ts`, context)
      }
      throw error
    }
  },
})
