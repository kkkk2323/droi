// Metro for the Phone App. Expo configures the monorepo itself; this only
// pins the packages that must exist once in the bundle to the Phone App's own
// copies. The shared daemon layer lives in packages/ and would otherwise
// resolve them next to the web Client, whose React is a different version.
const path = require('node:path')
const { getDefaultConfig } = require('expo/metro-config')

const config = getDefaultConfig(__dirname)

const SINGLETONS = ['react', 'react-dom', '@tanstack/react-query', '@factory/droid-sdk']
const fromApp = path.join(__dirname, 'package.json')
const upstream = config.resolver.resolveRequest
// The SDK's relay needs libsodium, which throws as it loads under Hermes; the
// Phone App has no relay (src/platform/libsodium-stub.ts).
const LIBSODIUM_STUB = path.join(__dirname, 'src/platform/libsodium-stub.ts')

config.resolver.resolveRequest = (context, moduleName, platform) => {
  if (moduleName === 'libsodium-wrappers-sumo')
    return { type: 'sourceFile', filePath: LIBSODIUM_STUB }
  const pinned = SINGLETONS.some((name) => moduleName === name || moduleName.startsWith(`${name}/`))
  const next = pinned ? { ...context, originModulePath: fromApp } : context
  return upstream
    ? upstream(next, moduleName, platform)
    : context.resolveRequest(next, moduleName, platform)
}

module.exports = config
