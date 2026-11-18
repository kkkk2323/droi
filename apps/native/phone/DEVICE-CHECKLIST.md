# Phone App device checklist

What only a real iPhone can show. The rest is covered by `go test ./apps/native/...`:
`internal/app/phone_test.go` draws this app's views at 402×874 against the Fake Daemon
(ADR 0040). Walk through this before calling a Phone App change done, with the iPhone connected
to this Mac and Droi running here with Remote Access on.

## Installing

- [ ] Developer Mode is on (Settings → Privacy & Security → Developer Mode).
- [ ] `pnpm install:phone` builds, signs and installs without opening Xcode (Xcode is signed in
      to the Apple account, and `xcodegen` is installed).
- [ ] First install only: the app opens after trusting the developer in
      Settings → General → VPN & Device Management.
- [ ] The app opens with no Mac process serving it: the Go code is inside the app, and it talks
      to the computer's Gateway alone.
- [ ] Settings → About shows the desktop app's version.
- [ ] Installing again over the app, which renews the free account's seven-day profile, keeps
      the Paired Computers: it connects without pairing again (they and their Pairing Tokens are
      in the app's `preferences.json`).
- [ ] Deleting the app and installing it again forgets them: it opens on the pairing page.
- [ ] An install over the old Expo Phone App opens on the pairing page once, with no Paired
      Computer to read.

## Pairing and connecting

Droi on the computer must be a build with the computer name and id in its `/meta` (a current
release, or `go run ./apps/native` from this repository).

- [ ] Scan QR code asks for the camera once and pairs from the code in
      Droi → Settings → Remote Access.
- [ ] Pasting the pairing link asks for local network access once, with Droi's explanation, then
      connects.
- [ ] The app opens on the Session list: the computer's name and connection in the bar, its
      Sessions by Workspace below.
- [ ] Opening a Session pushes it; a swipe from the left edge goes back to the list, and so does
      the "Sessions" button.
- [ ] Settings shows its sections as a page, each section as a page of its own, and the back
      button returns to the sections.
- [ ] Settings → Computer hides "This computer" and offers Scan QR code and the other Paired
      Computers.
- [ ] With the keyboard up, the composer sits right above it on a Session and on New session (no
      gap, nothing hidden).
- [ ] Quitting the app on a Session and launching it again shows that Session, with the list one
      swipe back.
- [ ] Sending the app to the background for a minute and back keeps the open Session on screen;
      it reconnects on its own (a Reconnecting banner at most).
- [ ] With Remote Access turned off on the computer, the app says it cannot reach it, lists what
      to check, and connects once it is back on.
- [ ] With a Relay set in Droi → Settings → Remote Access and shown as connected, scanning the
      new code pairs; with Wi-Fi off, on cellular, the Session list loads through the Relay and a
      reply streams in.
- [ ] A phone paired before the Relay was set, opened once on the computer's Wi-Fi, then on
      cellular, loads the Session list through the Relay without scanning again.
- [ ] Back on the computer's Wi-Fi, bringing the app to the foreground connects directly again:
      the Relay's log shows no new connection for it.
- [ ] After the computer slept and woke, the Relay row is connected again within half a minute,
      and the app reconnects on its own.

## Composer

- [ ] Add image opens the system photo picker and attaches one or several photos; a HEIC photo
      goes out as JPEG (the Session shows it, and the model reads it).
- [ ] Paste image attaches an image copied in another app.
- [ ] The composer does not take the focus when a Session opens, so its keyboard stays down until
      the field is tapped.

## Worktrees

- [ ] New session in a Git repository offers Worktree under the Workspace; a Session started
      with "New worktree" shows in the list under the repository with its branch, and
      `git worktree list` on the computer shows the checkout.
- [ ] Archiving a Session in an ephemeral worktree asks first, and the checkout is gone from the
      computer afterwards; a persistent one archives without asking.

## Workstreams

- [ ] With a Workstream started in Droi on the computer, the list shows its orchestrator with how
      its agents are doing, and none of the agents as Sessions of their own; an agent waiting on
      a permission says so within a few seconds.
- [ ] Opening the orchestrator shows its agents; tapping one pushes its Session, and going back
      returns to the orchestrator.

## Skills and MCP servers

- [ ] The wrench in a Session's header opens the sheet; switching a skill off shows in the droid
      CLI's `/skills` on the computer, and the "/" menu here.
- [ ] Adding a server: the keyboard does not cover the form's fields, and the new server shows in
      `~/.factory/mcp.json` on the computer.
- [ ] Sign in on a server that needs it: Copy sign-in link puts the page's URL on the clipboard;
      opened in the computer's browser it connects the server.

## Alerts

- [ ] With another Session open, a Session finishing plays the finished sound and a success
      haptic; one starting to wait for an answer plays the needs-input sound and a warning
      haptic.
- [ ] With the silent switch on, the haptic is still felt but no sound plays.
- [ ] Music from another app keeps playing under the alert sound.
- [ ] A Session that finished while the iPhone was locked shows its unread dot after unlocking;
      opening it in Droi on the computer clears the dot here within a few seconds, and opening it
      here clears it on the computer.

## Appearance

- [ ] With Theme on System, switching the iPhone between light and dark (Control Center)
      switches the app at once.
- [ ] With Theme on Dark while the iPhone is light, the keyboard is dark too.
