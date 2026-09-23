# Phone App device checklist

What only a real iPhone can show. The rest is covered by `pnpm test:e2e:phone`
(ADR 0006). Walk through this before calling a Phone App change done, with the
iPhone connected to this Mac and Droi running here with Remote Access on.

## Installing

- [ ] Developer Mode is on (Settings → Privacy & Security → Developer Mode).
- [ ] `pnpm install:phone` builds, signs and installs without opening Xcode.
- [ ] First install only: the app opens after trusting the developer in
      Settings → General → VPN & Device Management.
- [ ] The app opens without a dev server running (the JS bundle is inside).
- [ ] Settings shows the desktop app's version and "7 days left" right after
      installing; running `pnpm install:phone` again resets it.
