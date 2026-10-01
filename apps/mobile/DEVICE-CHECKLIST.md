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

## Pairing and connecting

Droi on the computer must be a build with the computer name and id in its
`/meta` (`pnpm install:mac` from this repository).

- [ ] Pasting the pairing link from Droi → Settings → Remote Access asks for
      local network access once, with Droi's explanation, then connects.
- [ ] The app opens on the Session list: the computer's name and connection in
      the navigation bar, its Sessions by Workspace below.
- [ ] Opening a Session pushes it; a swipe from the left edge goes back to the
      list, and so does the back button.
- [ ] The New session, Settings, Git changes, Tools and subagent buttons sit in
      the navigation bar and respond to a tap.
- [ ] With the keyboard up, the composer sits right above it on a Session and on
      New session (no gap, nothing hidden under the navigation bar).
- [ ] Quitting the app on a Session and launching it again shows that Session,
      with the list one swipe back.
- [ ] Scan QR code asks for the camera once and pairs from the code in
      Droi → Settings → Remote Access.
- [ ] After `pnpm install:phone` again, the Paired Computers are still there
      and connect without pairing again (the tokens live in the keychain).
- [ ] Sending the app to the background for a minute and back keeps the open
      Session on screen; it reconnects on its own (a Reconnecting banner at most).
- [ ] With Remote Access turned off on the computer, the app says it cannot
      reach it, lists what to check, and connects once it is back on.

## Composer

- [ ] Add image → Photo library attaches one or several photos (HEIC photos go
      out as JPEG); Take photo asks for the camera once; Paste image attaches an
      image copied in another app.

## Skills and MCP servers

- [ ] The wrench in a Session's header opens the sheet; switching a skill off
      shows in the droid CLI's `/skills` on the computer, and the "/" menu here.
- [ ] Adding a server: the keyboard does not cover the form's fields, and the
      new server shows in `~/.factory/mcp.json` on the computer.
- [ ] Sign in on a server that needs it: Copy sign-in link puts the page's URL
      on the clipboard; opened in the computer's browser it connects the server.

## Alerts

- [ ] With another Session open, a Session finishing plays the finished sound
      and a success haptic; one starting to wait for an answer plays the
      needs-input sound and a warning haptic.
- [ ] With the silent switch on, the haptic is still felt but no sound plays.
- [ ] Music from another app keeps playing under the alert sound.

## Appearance

- [ ] With Theme on System, switching the iPhone between light and dark
      (Control Center) switches the app at once.
- [ ] With Theme on Dark while the iPhone is light, the keyboard is dark too.
