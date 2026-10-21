# Droi native (MyGo)

The Desktop Shell and its window rewritten in Go on [MyGo](https://github.com/egoist/mygo)'s
native UI: no Electron, no webview, no Node at run time. Its Synara-inspired workbench uses a
grey navigation rail, a separate reading surface, compact system typography and inset composers
(ADR 0025).

It replaces the Electron app: same bundle identifier (`com.droi.app`), name
and data folder (`~/Library/Application Support/Droi`), so the Shell
settings, the Pairing Token and Memory carry over. The window talks to the
Daemon through its own Host's Gateway with [droid-sdk-go](https://github.com/kkkk2323/droid-sdk-go), as a Local
Client with a token made at each launch; the Gateway supplies the credential and
the System Prompt Addition, as it does for other Clients.

How it performs next to the Electron app: [docs/performance.md](docs/performance.md). How
it is released and updates itself: [docs/releases.md](docs/releases.md).

## droi host on Linux

On Linux Droi has no window: `droi host` runs the Host (the Daemon, the Gateway with Remote
Access always on, and Memory) in the foreground, for a headless computer your phone or another
Client reaches (ADR 0019). Run `droi` without `host` there and it says so and exits.

**Download.** Each [Release](https://github.com/kkkk2323/droi/releases/latest) has
`droi-<version>-linux-amd64.tar.gz` and `droi-<version>-linux-arm64.tar.gz`, for glibc
systems. Extract one anywhere; the program is `droi`:

```sh
ARCH=amd64   # arm64 on ARM
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/kkkk2323/droi/releases/latest | sed 's|.*/v||')
curl -fsSLO https://github.com/kkkk2323/droi/releases/download/v$VERSION/droi-$VERSION-linux-$ARCH.tar.gz
sudo mkdir -p /opt/droi && sudo tar -xzf droi-$VERSION-linux-$ARCH.tar.gz -C /opt/droi
```

**First start.** Install the [Droid CLI](https://docs.factory.ai) (Droi finds `droid` in
`~/.local/bin` as well as on PATH), then sign in the way `droid` does: run `droid login` on
this computer, or set `FACTORY_API_KEY`. Droi reads the CLI's login from its keyfile, then from
the system keyring (through `secret-tool`, when it is installed), then falls back to
`FACTORY_API_KEY`. Start it:

```sh
/opt/droi/droi host
```

It prints `Pairing link: http://<address>:<port>/#pair=<token>` and, on a terminal, the same
link as a QR code. Scan it with the Phone App, or give the link to another Client. A warning
says so when there is no login yet; sign in and restart. It stops on Ctrl-C or SIGTERM, and
refuses to start while another Droi Host (the app, or another `droi host`) answers on this
computer, or when `droid` is missing or older than Droi needs.

The Gateway listens on port 41417; `droi host --port N` picks another, for instance when the
computer already runs a Host there. In a container, publish the same port on both sides
(`docker run -p 41418:41418 ... /opt/droi/droi host --port 41418`) and set `pairingHost` (see
below) so the link names a port and an address other computers reach.

Its settings (`settings.json`, mode 0600, with the Pairing Token), Memory and logs are in
`$XDG_CONFIG_HOME/droi` (`~/.config/droi` by default). Scratch folders a Client trashes go to
the freedesktop Trash in `$XDG_DATA_HOME/Trash`.

**Keep it running with systemd.** As a user service (`~/.config/systemd/user/droi-host.service`):

```ini
[Unit]
Description=Droi Host
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/droi/droi host
# FACTORY_API_KEY=... in this file (mode 0600), if you do not use `droid login`
EnvironmentFile=-%h/.config/droi/env
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now droi-host
loginctl enable-linger "$USER"   # start at boot, without a login
journalctl --user -u droi-host -f   # the pairing link is in the log
```

For a system service, put the unit in `/etc/systemd/system`, add `User=<you>` to `[Service]`
(Droi needs that user's home for its settings and for `droid`), use `WantedBy=multi-user.target`,
and run `systemctl enable --now droi-host`.

**Reset the Pairing Token.** Stop the Host, delete `pairingToken` from
`~/.config/droi/settings.json` (`jq 'del(.pairingToken)' settings.json` does it), and start
it: it makes a new token, prints the new link and every phone paired before has to pair again.

**Keep it on a private network.** The Gateway listens on every network interface of the
computer and speaks plain HTTP, so a Pairing Token is the only protection. Reach the Host over a
private network such as [Tailscale](https://tailscale.com), and do not expose its port to the
internet. The link names the computer's first LAN address; to have it name another, for
instance the Tailscale machine name, set `"pairingHost": "my-box"` in `settings.json` and
restart.

## Following another computer

Settings → Computer lists this computer and every other computer the window has followed,
marks the current one and switches to any other with one click. A computer is added by pasting
the pairing link a phone would scan (the link `droi host` prints, or the one in Settings →
Remote Access of another Droi): Droi checks that the link reaches a Droi Gateway that accepts
its Pairing Token, remembers it as a Client preference (`droi.computers` in `preferences.json`,
which takes over the single `droi.computer` of earlier versions) and switches to it. A new link
for a computer already listed replaces its address and token. The window then connects to that
Gateway instead of its own, lists that computer's Sessions and hides the Account, Memory,
Remote Access and Advanced settings, which belong to this computer's Host. This computer's
Host keeps running for its phones and Automations. "Use this computer" goes back and keeps
the others listed; the trash button forgets one. When the followed computer cannot be reached
(it is off, or its Pairing Token was reset), the window offers to use this computer, paste a new
link or forget it. Once another computer is remembered, the top left of the sidebar, beside its
toggle, names the computer shown; its menu switches to any other or opens Settings → Computer.

A switch does not relaunch the app. `main.go` keeps the window open and builds its insides
again (`inside.go`): a new Controller to the computer's Gateway, a new Gateway Client and a new
`app.App`, started and connected; the old App stops hearing its Controller and its calls end
(`App.Close`), then the old Controller closes. The menu, zoom, notification clicks and the
updater's wait for an idle window always go to the current App. Relaunching stays for updates.

Preferences that name Workspaces by path (pinned and folded Workspaces, their manual order, the
preferred Open in app) are kept per computer: this computer's stay under their old keys in
`preferences.json`, so an update reads them unchanged, and another computer's sit under the same
key with `@` and the computer id added. Drafts and Session stats are keyed by Session id and need
no split.

## Layout

| Path                                                                                      | What                                                                                                  |
| ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `main.go`                                                                                 | the app: window, menus, the Host                                                                      |
| `assets/`                                                                                 | the vendored JetBrains Mono and Geist fonts, embedded                                                 |
| `pkg/theme/`                                                                              | the design tokens (ADR 0025): the three themes, fonts, text sizes, the one tint                       |
| `pkg/icons/`                                                                              | the Lucide icons the app draws (`scripts/gen-icons.mjs`)                                              |
| `pkg/host/`                                                                               | the Host: Shell settings, Daemon supervisor, Factory sign-in, the droid CLI's login, Magpie's models  |
| `internal/memory/`                                                                        | Memory's store (SQLite, the Electron app's schema), Memory Server, hook, Markdown export              |
| `internal/memorywork/`                                                                    | Memory Sessions: consolidation, extraction, prompts, what Settings → Memory shows                     |
| `pkg/gateway/`                                                                            | the Gateway for paired phones: proxy to the Daemon, pairing link, Scratch and session-file endpoints  |
| `internal/computer/`                                                                      | the Computer preference: the Paired Computers remembered, the one followed, and checking a link       |
| `internal/updates/`                                                                       | the in-app update: check, download, restart (MyGo's signed updates)                                   |
| `pkg/prefs/`                                                                              | the Client's local preferences (localStorage's place)                                                 |
| `pkg/transcript/`                                                                         | messages to transcript entries, tool calls, Script runs, json-render (daemon-layer port)              |
| `pkg/sessions/`                                                                           | the Session list: grouping, sorting, pins, activity (daemon-layer port)                               |
| `internal/automations/`                                                                   | the Automations page's schedules (local time to the Daemon's UTC cron), labels and ids                |
| `pkg/models/`, `pkg/defaults/`, `internal/skills/`, `internal/mcp/`, `internal/slash/`, … | models, Session Defaults, skills, MCP, slash items and the rest of the daemon layer, one package each |
| `pkg/kit/`                                                                                | the shadcn/Base UI look as MyGo elements: buttons, menus, selects, switches, dialogs, setting rows    |
| `pkg/sessionview/`                                                                        | one Session: transcript, Markdown, tool calls, Prompts, sending, a plain composer (no `*App`)         |
| `pkg/mermaid/`                                                                            | Mermaid flowcharts and sequence diagrams laid out for the Painter (after Ondine and Pele)             |
| `pkg/settingsview/`                                                                       | Settings sections: General's Appearance, Session defaults, the model picker (no `*App`)               |
| `internal/app/`                                                                           | the views: shell, sidebar, the Session's header, turn rail and composer, New session, settings        |
| `testdata/reference/`                                                                     | the web Client's screens at 2x with computed styles, recorded before that Client was removed          |

## Commands (from the repository root)

```sh
go run ./apps/native                 # run it (needs droid on PATH)
go test ./apps/native/...            # unit tests, view tests, Fake Daemon tests
go tool -modfile=apps/native/go.mod mygo build apps/native   # .app and .dmg
node apps/native/scripts/gen-icons.mjs [icon-name ...]   # add Lucide icons, or rewrite those in pkg/icons/svg
```

## Status

Tracked in this file while the port is under way.

- [x] Phase 0: SDK in the repo (`packages/droid-sdk-go`), module, tokens, fonts, icons
- [x] Phase 1: reference screens recorded
- [x] Phase 2: Host (settings, supervisor, sign-in, CLI login, Scratch)
- [x] Phase 3: shell, sidebar, search, routes, connection, `main.go`
- [x] Phase 4: Session view: transcript, Markdown, code, tools, thinking, subagent cards, todos, Scripts
- [x] Phase 5: composer, model picker, attachments, slash, queue, prompts, New session
- [x] Phase 6: Settings, Skills/MCP, git changes, Open in, copy Session, alerts (sound, desktop
      notification, unread mark), the subagent menu and trail
- [x] Phase 7: view tests against the Fake Daemon (`internal/app/app_test.go`) with a pixel
      comparison per reference screen
- [x] Phase 8: replaces the Electron app: identity and data folder; zoom, pasted images, the
      turn rail, custom alert sounds, the sign-in banner, "droid was updated", the start-up screen
- [x] Phase 9: in-app update (signed delta updates, restart when idle); the Electron app's last
      update points at the native one
- [x] Phase 10: Memory (store, Memory Server, hook, Memory Sessions, Settings → Memory), on the
      Electron app's data; the app's own binary is the Memory Server (`Droi memory-server`) and
      the hook (`Droi memory-hook`)
- [x] Phase 11: Remote Access and pairing for the Phone App (Gateway, Settings → Remote Access);
      no web Client in the Gateway

### How close it is

The reference screens are the removed web Client's, and Droi no longer looks like it on purpose:
the window wears another design (ADR 0025), so what these numbers measure is now how far the app
has moved from that Client, not how faithful the port is.

`DROI_NATIVE_SHOTS=/tmp/shots go test ./apps/native/internal/app -v` writes each screen and a
diff (red: a channel off by more than 24 of 255) next to the share of pixels that differ.

The [PR #164 screen gallery](../../docs/pr-164/README.md) shows the current Light, Dark and
Solarized Light+ surfaces, including Settings with its switch on and off. Those images are review
evidence; the removed Client's reference images remain unchanged.

Settings leaves out Account, Memory, Remote Access and Advanced when there is no Host (the
tests); the app has them all.

Live tests against a real Daemon, through droid-proxy, skip without a key:

```sh
FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') \
  go test ./apps/native/pkg/host ./apps/native/pkg/gateway -run Live -v
```
