// The Phone App's Expo config. The version is the desktop app's, read from
// apps/desktop/package.json at build time, so both come from the same release.
import type { ConfigContext, ExpoConfig } from 'expo/config'
import desktopPackage from '../desktop/package.json'

// Plain http is otherwise refused for names other than IPs, .local and ts.net.
// DROI_HTTP_DOMAINS (comma separated, e.g. "myhome") adds the suffixes of a
// private DNS such as Surge's, so a personal domain stays out of the repo.
const httpDomains = ['ts.net', ...(process.env['DROI_HTTP_DOMAINS'] ?? '').split(',')]
  .map((domain) => domain.trim().replace(/^\.+/, ''))
  .filter(Boolean)

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: 'Droi',
  slug: 'droi-phone',
  version: desktopPackage.version,
  // expo-router will not start in a standalone build without a scheme. Only
  // the router uses it; pairing stays the Desktop Shell's http link.
  scheme: 'droi-phone',
  orientation: 'portrait',
  icon: './assets/icon.png',
  userInterfaceStyle: 'automatic',
  ios: {
    bundleIdentifier: 'com.kkkk2323.droi',
    // Set by scripts/install-phone.sh from the signing certificate.
    appleTeamId: process.env['APPLE_TEAM_ID'],
    // The iPhone layout runs on an iPad; there is no iPad layout.
    supportsTablet: false,
    infoPlist: {
      NSLocalNetworkUsageDescription:
        'Droi connects to the Droi app on your computers over your local network.',
      NSAppTransportSecurity: {
        // The Gateway speaks plain http on the LAN; ATS allows that for IP
        // addresses and .local names here, and for the domains below. Not
        // NSAllowsArbitraryLoads: iOS ignores it while NSAllowsLocalNetworking is set.
        NSAllowsLocalNetworking: true,
        NSExceptionDomains: Object.fromEntries(
          httpDomains.map((domain) => [
            domain,
            { NSIncludesSubdomains: true, NSExceptionAllowsInsecureHTTPLoads: true },
          ]),
        ),
      },
      ITSAppUsesNonExemptEncryption: false,
    },
  },
  web: {
    // Only for the Playwright suite; never served to a phone.
    output: 'single',
    bundler: 'metro',
  },
  experiments: {
    // tsconfig's paths only steer React's types (see tsconfig.json); Metro
    // must not follow them.
    tsconfigPaths: false,
    // Components memoize themselves, as in the web Client.
    reactCompiler: true,
  },
  plugins: [
    'expo-router',
    'expo-font',
    'expo-secure-store',
    ['expo-audio', { microphonePermission: false }],
    [
      'expo-camera',
      {
        cameraPermission: 'Droi uses the camera to scan the pairing code shown on your computer.',
        microphonePermission: false,
        recordAudioAndroid: false,
      },
    ],
    [
      'expo-image-picker',
      {
        photosPermission: 'Droi attaches the photos you pick to your message.',
        cameraPermission: 'Droi attaches the photos you take to your message.',
        microphonePermission: false,
      },
    ],
    [
      'expo-splash-screen',
      {
        backgroundColor: '#ffffff',
        dark: { backgroundColor: '#161616' },
        imageWidth: 120,
        image: './assets/icon.png',
      },
    ],
  ],
})
