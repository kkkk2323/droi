---
status: accepted
---

# The Phone App is a native Expo Client that shares the web Client's daemon layer

On an iPhone the Remote Client has so far been the web Client in Safari. We decided iOS gets its own native Client, the Phone App, built with Expo and React Native, and that it takes the browser's place there; the browser Remote Client stays for every other device. The Phone App offers what the web Client offers (pairing, Session list, transcript, composer, Prompts, New session, Git changes, Session management, settings) and nothing the desktop lacks, so no terminal. It still speaks only the Daemon protocol through the Gateway (ADR 0001) and reads conversation state from the SDK's session controller (ADR 0004).

The repository becomes a pnpm workspace: `apps/phone` holds the Expo app, and the web Client's daemon layer (`src/renderer/src/daemon/` with the pure helpers it imports) moves into a shared package that both Clients import. That layer is already almost free of DOM access, and sharing it means Draft Sessions, Prompts, compaction and the connection state machine are implemented once. The screens are written twice: React DOM with Tailwind for the web Client, React Native with `StyleSheet` and theme constants translated from `global.css` for the Phone App.

## Considered Options

- **Keep the browser Remote Client and improve it (web push from the home screen).** Least work, but the user wanted a native feel on the phone.
- **Wrap the web Client in a native shell (Capacitor).** Reuses every screen, but the result is still the web page, which is the thing being replaced.
- **Copy the daemon layer into the Phone App, or keep the Phone App in its own repository.** Both let the two Clients drift apart on behaviour that must match; the shared package in one repository was chosen instead.
- **NativeWind or Expo DOM components to reuse the web markup.** NativeWind trails Tailwind v4 and new Expo SDKs; a WebView per message is too slow for a long transcript. Markdown gets its own renderer on `mdast-util-from-markdown`.

## Consequences

- The Phone App is for personal use: it is signed with a free Apple account, so it cannot receive push notifications and its signature lapses after seven days. `pnpm install:phone` rebuilds and installs it, and its settings show the days left. Alerts (haptics and two short sounds of Droi's own) fire only while the app is open.
- The Phone App is pinned to Expo SDK 57, built with Xcode 27 on macOS 27. The iOS 27 SDK requires the scene-based life cycle, which SDK 57 only turns on through `expo-build-properties` (`ios.enableSceneSupport`); SDK 58 makes it the default.
- The Phone App is connected to one Paired Computer at a time and keeps only a summary of each computer's Session list between launches; transcripts always come from the Daemon. To tell computers apart, the Gateway's `/meta` reports the computer's name and a stable id, so scanning a computer again updates its Paired Computer instead of adding one.
- The Phone App's behaviour is tested the way the web Client's is (ADR 0002): its web build (react-native-web) runs under Playwright against the Fake Daemon, with web stand-ins for the camera, the keychain and haptics. That build exists only for tests and is never served to a phone. What only a device can show (scanning, the keychain, haptics, the silent switch, resuming from the background) is checked by hand against a checklist.
- The Phone App carries the desktop version number and warns, without blocking, when `/meta` reports a different one.
- Waku's mobile app is licensed GPL-3.0 and Droi MIT: Waku serves as a reference for the design, and none of its code is copied.
