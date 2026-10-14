---
status: accepted
---

# The Host runs on its own as `droi host`, and the window reaches every Host through a Gateway

ADR 0012 planned the Host as an npm package (`droi-host`) beside the Electron Desktop Shell, with a command line, self-update, a Pinned Droid, a management surface on the Gateway and a list of Paired Computers in the window. The native app replaced Electron before any of it was built. Its Host, Gateway and Memory already have no window code and build for Linux, but its window talks to the Daemon directly and reads this computer's disk, so it can only ever show this computer. Meanwhile Factory's Remote Control (droid 0.238) reaches any computer's Daemon through Factory's relay, which covers "a headless box I can reach from anywhere" for anyone who does not need Droi's own features there.

We decided to take Waku's shape (`~/dev/waku`: the desktop is always an RPC client of its daemon, a remote daemon is one target chosen at launch, and one remote check hides what only works on the local machine) and to build only that:

- **The window is a Client of a Gateway, also on its own computer.** It connects to its own Host's Gateway over loopback with a per-launch Local Token, as ADR 0001 intended. The Gateway alone adds the credential and the System Prompt Addition, and Scratch Workspaces and session files go through its endpoints for every Client. There is no direct fallback: a Gateway that cannot listen is an error the window shows. The Host's own Memory Sessions still dial the Daemon directly; they are not a Client.
- **A Host on its own is `droi host`**, the same Droi program without a window, run in the foreground and kept alive by the system's service manager. It prints the pairing link and QR code when it starts; its settings are a file, and deleting the Pairing Token there and restarting resets it. It is offered and documented for Linux only, where releases carry amd64 and arm64 builds; on macOS and Windows the app is the Host, and `droi host` refuses to start where a Droi Host already runs. It uses the computer's `droid login` (the keyfile, then the system keyring, then `FACTORY_API_KEY`), keeps its data under `$XDG_CONFIG_HOME/droi` and trashes into the freedesktop Trash.
- **The window follows one computer at a time.** Settings has a Computer entry: this computer, or one other Paired Computer added with its pairing link, kept as a Client preference. Switching relaunches the app. The window's own Host keeps running meanwhile, so paired phones and local Automations are unaffected. While it shows another computer, the window hides what belongs to its own Host or disk (the Account, Memory, Remote Access and Advanced settings, opening folders in other apps, local folder pickers), shows full paths, attaches only pictures, keeps path-keyed preferences per Paired Computer, and offers to go back to this computer when the other cannot be reached.

Not built: the npm package and its `setup`/`status`/`logs`/`config` commands, switching without a relaunch, more than one remote computer in the window, restarting or updating a Host from a Client, Host self-update, and Tailscale-first binding (the Gateway keeps listening on every LAN interface; the Pairing Host names the address). The Pinned Droid stays deferred (ADR 0018). Transport security is unchanged: outside a private network such as Tailscale, keep Remote Access off.

## Considered Options

- **ADR 0012 as written.** Eight tickets of command line, updater and management for one person's two computers.
- **Factory's relay for the window.** The Mac's Host holds a Factory login, so it could; but the relay protocol is private and changing, it checks computer ownership per Factory account, and it would bypass every Gateway feature. The Phone App could not use it without holding a Factory credential (ADR 0001).
- **Waku's environment variables for the remote target.** No UI at all; rejected for a Settings entry that takes the same pairing link a phone scans.

## Consequences

- Updating changes nothing a user configured: the window's switch to the Gateway is invisible, the Gateway keeps its port, phones stay paired, and Computer starts on this computer. `droi host` and a remote Computer are opt-in.
- The window now exercises the Gateway path every day, which the Phone App shares.
- Existing path-keyed preferences belong to this computer; another computer's start empty.
