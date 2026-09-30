---
status: accepted
---

# The Host runs on its own, and Clients connect to many Hosts directly

Until now everything that is not a window lived in the Electron main process: spawning the Daemon, the Gateway, the Factory credential, Memory. The Daemon itself is Factory's `droid daemon` and has always been able to run anywhere, but nothing could reach it without a Desktop Shell on the same computer. The user's shape is different: a headless Ubuntu box that should run Sessions, a Mac that sometimes runs its own and sometimes drives the Ubuntu box, and an iPhone that reaches either. All devices are on one Tailscale network.

We decided to name the non-window part the **Host** and make it a package of its own (`droi-host` on npm, `packages/host` in the repo) with a command line for a computer without a Desktop Shell: `setup`, `start`, `stop`, `restart`, `status`, `logs`, `pair`, `config`, `update`. The Desktop Shell embeds the same package and injects what only Electron has (the user-data path, the version, the system trash, notifications). The topology stays **direct**: each computer runs one Host, every Client keeps a list of Paired Computers and connects to one at a time through that Host's Gateway. There is no rendezvous server between them; Tailscale is the network and the Pairing Token is the second lock.

We looked closely at multica (`~/dev/multica`), whose daemons dial out to a central server and whose clients never see a daemon. That buys NAT traversal and multi-user dispatch, both of which Droi does not need: it is a single person's tool on a private network, and a server would turn a thin Client (ADR 0001) into a platform. What we take from multica is the daemon's operational shape: a pid file and log per install, `--foreground` for debugging, following a replaced binary on disk, and restarting only when idle (ADR 0013).

## Considered Options

- **Keep the Shell, run Electron headless (xvfb) on Ubuntu.** No new package, but a display server and a 200 MB runtime for what is a Node program; every Shell setting would still need a window to change.
- **A central server that Hosts register with (multica's shape).** Rejected as above.
- **Reach a remote Daemon by SSH port-forward from the Mac's own Gateway.** Works for one Mac and one box, keeps the credential on the Mac, but the Daemon needs its own Factory identity anyway, nothing supervises it, and the phone still needs the Mac to be on.
- **Host as a separate process (chosen).** The main-process code was already Electron-free except for one wiring file, with unit tests.

## Consequences

- `CONTEXT.md` gains Host and changes Desktop Shell, Gateway, Pairing Token, Paired Computer, Remote Access, Local Client and Remote Client. A Local Client is the Shell window whichever Host it shows; only it reaches Shell settings, and those settings are always the embedded Host's.
- The Gateway grows a small management surface for what a Client may do to a Host from afar: restart the Daemon, update the Host, read the Daemon log tail, and a richer `/meta` with versions and Daemon state. Remote Access, the Pairing Token and the Factory credential are never changed remotely.
- A Host on its own has Remote Access always on and binds the Tailscale address by default, falling back to the LAN interfaces with a warning; the Desktop Shell's Host follows the same binding rule while keeping its Remote Access switch.
- The Host's Factory identity is normally the droid CLI's login on that computer (`droid login` once on the Ubuntu box), read the way the Desktop Shell already reads it; an API key in the Host's configuration is the fallback. droid-proxy works unchanged behind either: it rewrites only the LLM calls' key and passes identity through.
- Each Host has its own Memory, Scratch Workspaces, `~/.factory` (Sessions, Session Defaults, skills, MCP servers). Nothing is synchronised between computers; the Client shows whichever Host it is connected to.
- The web Client served by a Host in a browser stays bound to that Host; only the Desktop Shell window and the Phone App carry a list of Paired Computers.
- Client preferences that name Sessions or Workspaces (pins, manual order, first-seen times, drafts) are kept per Paired Computer, as the Phone App already does; appearance preferences stay per Client.
- Windows gets a Host only through the Desktop Shell; the command line ships for Linux and macOS.
