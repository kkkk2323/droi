---
status: superseded by ADR-0041
---

# The Phone App is the native app's window on MyGo

Since ADR 0006 the Phone App has been a Client of its own: TypeScript on Expo and React Native,
sharing a daemon layer with the web Client that was removed after 1.59.0, and with nothing else.
That left two implementations of everything the desktop window shows: the Session list, the
transcript, the composer, Prompts, New session, Settings, Worktrees, Workstreams, Skills and MCP
Servers. Every desktop feature had to be written twice, and some were never ported at all: the
phone had no Automations, Loops or Usage page, and its Settings were a short list of its own.

MyGo 0.10.0, tagged 17 November 2026, gave MyGo's window an iOS and Android backend (Droi
builds the iOS one): the same view tree is drawn with Metal and Core Text on the phone, and the
plugins a desktop app calls (clipboard, haptics, native modules written in Swift) are there
too. Droi's window is already a MyGo app, and its views already run in the tests without a
window, against the Fake Daemon.

We decided:

- **The Phone App is that window.** `apps/native/phone` builds `app.App` from `internal/app`
  with `Config.Phone`, following one Paired Computer through its Gateway, with no Host of its
  own (`Config.Host == nil`). It is in the native app's Go module, so it reuses the window's
  views, the SDK, the Gateway client and the Computer preference as they are.
- **Phone mode is the window, always narrow.** `home` is the Session list full screen, and a
  Session, the New session page and Automations push over it with a back button (a chevron-left
  named "Sessions") and a swipe from the left edge; there is no drawer (ADR 0014's
  list-then-push is kept). Settings shows its sections as a page and each section as its own
  page, as any narrow window does. The composer does not take the focus, which would bring the
  keyboard over the transcript. Settings → Computer hides "This computer" and adds "Scan QR
  code".
- **The window grows the hooks a phone needs.** `Config` gained `Phone`, `PickImages`,
  `ScanQR` and `Alerted(waiting, sound)`, so the window asks for photos, a code and a haptic
  without knowing iOS; `app.PrefsKit` and `app.ComputerError` give the pairing page its kit and
  its errors.
- **What has no counterpart in Go is a Swift plugin.** `phonekit` is a MyGo mobile plugin
  (`mygo-plugin.json`, `ios/PhoneKitPlugin.swift`): the camera with `AVCaptureMetadataOutput`
  for the pairing code, `PHPicker` for photos, each re-encoded as JPEG with its longest side at
  1568 pixels and quality 0.85 so HEIC goes out as JPEG, and `AVAudioSession`'s ambient
  category for the alert sounds.
- **Paired Computers are the desktop's record.** The phone keeps them, Pairing Token and all, in
  its `preferences.json` through `internal/computer`, not in the Keychain as the Expo app did:
  one store, one shape, one way to check a link. The pairing page scans or pastes a link, lists
  the Paired Computers to reopen one or forget it, and is what the app shows before any is set.
- **It arrives as the same app.** The bundle identifier stays `com.kkkk2323.droi`, so the build
  installs over the Expo app, and the version is the desktop's, checked by a test.

## Considered Options

- **Keep the Expo Client (ADR 0006) and catch it up.** Every feature stays written twice, and the
  phone trails the desktop by however long the port takes, which is what this decision ends.
- **A separate Go phone UI on top of `pkg/sessionview` only** (a phone-shaped Session view, no
  sidebar, Settings or Workstreams). Less code than `internal/app`, but it is still a second
  Client to write and keep in step, and "nothing the desktop lacks" would go the other way: the
  phone would lack what the window has.
- **A separate Go module for the phone**, with `internal/app` exported for it. It would keep the
  phone's binary and dependencies apart, at the price of a second module to version with the
  desktop, and of exporting the window's internals for exactly one caller.
- **Wait for MyGo's mobile support to settle.** The Expo Client would keep working meanwhile,
  but the features missing from it stay missing.

## Consequences

- MyGo's mobile support is new: 0.10.0 is its first release with it, so the pin will move often
  and a phone-only bug is as likely to be MyGo's as Droi's. The desktop app is unaffected.
- The desktop app still builds with `CGO_ENABLED=0`; the iOS build links its Go with Xcode's
  clang, so it needs Xcode and cgo. `go tool mygo build -platform ios/simulator phone` builds
  it, and `pnpm install:phone` signs and installs it: it builds a throwaway Xcode project so
  Xcode creates the free account's development profile, which needs `xcodegen` and Xcode signed
  in to the Apple account.
- The window's controls are MyGo's own drawing rather than UIKit widgets, and its page stack,
  back button and edge swipe are Droi's; only system dialogs, the camera, the photo picker and the
  haptics are the platform's. A MyGo fix reaches both apps at once, which is the point.
- Math in a transcript is drawn as Unicode where it can be and as its TeX otherwise
  (`pkg/sessionview/math.go`), not as MathJax's SVG as the Expo Client showed it.
- Paired Computers live in the app's preferences: installing over the app keeps them, deleting the
  app forgets them, and the first install over the Expo app pairs once. There is no Keychain to
  keep them in, and the token is as protected as `preferences.json` is.
- Alerts are a haptic always and a sound when it is on; the ambient category keeps the silent
  switch and other apps' audio as they are. With no push (the app is signed with a free Apple
  account) they fire only while the app is open, and the app reconnects by itself when it comes
  back: the network address first, the Relay after two seconds (ADR 0034).
- The Expo app, the shared daemon layer and the Playwright suite are gone: `apps/mobile`,
  `packages/daemon-layer` and `tests/`, with `pnpm test`, `pnpm typecheck` and
  `pnpm test:e2e:phone`. The pnpm workspace is left with oxlint and oxfmt.
- The Phone App's views are the window's tests, with `Config.Phone` on a 402×874 screen
  (`internal/app/phone_test.go`); the PR workflow builds the app for the iOS simulator, and what
  only an iPhone can show is walked by hand (`apps/native/phone/DEVICE-CHECKLIST.md`).
- ADR 0006 is superseded; ADR 0014's navigation stands.
