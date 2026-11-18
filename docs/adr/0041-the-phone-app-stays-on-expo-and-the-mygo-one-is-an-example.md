---
status: accepted
---

# The Phone App stays on Expo, and the MyGo one is an example

ADR 0040 made the Phone App the desktop window drawn by MyGo on the iPhone, and 1.98.0 shipped
it in place of the Expo app. On the iPhone it was good enough to measure well (less memory, a
smaller app, the same CPU while scrolling), but not to keep: scrolling needed three fixes of our
own in MyGo's iOS surface and touch handling (egoist/mygo#2002, #2003, #2004), MyGo 0.10.0 is
its first release with mobile support, and phone-only problems kept turning up in the composer,
the model picker and the safe areas. The view tests of the phone layout raced the Fake Daemon on
one CPU, too. The Expo app, which has served since ADR 0006, has none of these problems.

We decided:

- **The Phone App is the Expo Client again** (ADR 0006): `apps/mobile`, `packages/daemon-layer`
  and `tests/mobile` are back as they were in 1.97.1, with `pnpm test`, `pnpm typecheck`,
  `pnpm test:e2e:phone` and `pnpm install:phone`.
- **`apps/native/phone` stays as an example.** The code, its view tests
  (`internal/app/phone_test.go`), `Config.Phone` and the PR workflow's iOS simulator build stay,
  so it keeps building while the window changes. `pnpm install:phone:mygo` installs it. New
  Phone App features go to `apps/mobile`; the example only has to keep building and passing its
  tests.
- **We switch again when MyGo's mobile support matures:** when a MyGo release has our three
  fixes, scrolling and input feel native on a real iPhone, and the phone view tests are steady.

## Considered Options

- **Stay on MyGo and fix what turns up.** The goal of ADR 0040 (one window, no second Client)
  stays right, but the phone would keep waiting on a young framework, and every fix would be a
  fork of MyGo.
- **Delete the MyGo phone.** The least code to keep, but the work of ADR 0040 would have to be
  redone, and the hooks it added to the window (`Config.Phone`, `PickImages`, `ScanQR`,
  `Alerted`) would rot.

## Consequences

- Every Phone App feature is again written twice, once in Go for the window and once in
  TypeScript for the phone, until the switch.
- Both apps have the bundle identifier `com.kkkk2323.droi`: installing one replaces the other,
  and each pairs once after the switch, since the Expo app keeps Paired Computers in the
  Keychain and the MyGo one in `preferences.json`.
- `apps/native/go.mod` keeps the MyGo fork with the iPhone fixes until a MyGo release has them;
  the desktop app is not affected by them.
- ADR 0040 is superseded; its notes stay as the plan for the switch.
