# Droi native (MyGo)

The Desktop Shell and its window rewritten in Go on [MyGo](https://github.com/egoist/mygo)'s
native UI: no Electron, no webview, no Node at run time. It looks the same as
the web Client (`apps/desktop/src/renderer`) running as the Local Client, pixel
for pixel where it can.

It replaces the Electron app: same bundle identifier (`com.droi.app`), name
and data folder (`~/Library/Application Support/Droi`), so the Shell
settings, the Pairing Token and Memory carry over. The window talks to the
Daemon through its own Host's Gateway with `packages/droid-sdk-go`, as a Local
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

**Turn on the Chat Bridge.** To answer a WeCom robot from this computer (see
[Chat Bridge](#chat-bridge-droid-in-wecom)), stop the Host, add a `chatBridge` block to
`~/.config/droi/settings.json` and start it again; it reads the block only at start:

```json
"chatBridge": {
  "botId": "aib...",
  "secret": "...",
  "workspace": "/home/me/src/acme",
  "allowedUsers": ["zhangsan"],
  "autonomyLevel": "low",
  "model": ""
}
```

`botId` and `secret` are the robot's; an empty `botId` leaves the Chat Bridge off. `workspace` is
the one folder every Chat Session runs in. `allowedUsers` are the WeCom user ids that may talk
to the robot. `autonomyLevel` is `off`, `low` (the default when empty), `medium` or `high`, and
an empty `model` uses the Session Defaults. The Host adds `refusedUsers` itself: the ids of
senders it turned away, newest last, so you can move one to `allowedUsers` and restart. The
settings file is mode 0600, which keeps the Secret yours. The log says `[chat] connected to
WeCom`, or why not.

**Keep it on a private network.** The Gateway listens on every network interface of the
computer and speaks plain HTTP, so a Pairing Token is the only protection. Reach the Host over a
private network such as [Tailscale](https://tailscale.com), and do not expose its port to the
internet. The link names the computer's first LAN address; to have it name another, for
instance the Tailscale machine name, set `"pairingHost": "my-box"` in `settings.json` and
restart.

## Chat Bridge: Droid in WeCom

The Chat Bridge (ADR 0020) connects one WeCom (企业微信) smart robot to this computer's Host. When
someone @s the robot in a post of a WeCom Channel (频道), or writes to it in a direct message,
the Host runs what they ask as a Chat Session in one fixed Workspace, and Droid answers there
with the `droi-chat` tools: it reads the post and its replies, searches the Channel, downloads
attachments, replies with files and @s, and reacts with an emoji. Each post is its own Chat
Thread, and so is each person's direct message; `!new` (in a post, `@robot !new`) starts the
Chat Thread over. A permission request becomes a card with a button per option in a direct
message, and a numbered reply in a post, answered with `@robot <number>`; the first answer
wins, in WeCom or in any Client. Chat Sessions show in the Session list with a robot mark. The
robot uses WeCom's long connection, so the computer needs no public address.

**Create the robot.** In WeCom, open 工作台 → 智能机器人 → 新建机器人 (an administrator may have to
allow it in the admin console). On the robot's edit page switch to **API 模式** (API mode) and
choose **长连接** (long connection), not URL 回调. The robot's details page then shows its
**Bot ID** and **Secret**; copy both and keep the Secret private.

**Add it to a WeCom Channel.** Open the Channel (Channels need WeCom 5.0.12 or later; the
feature is still in gray release) and add the robot from the robots listed under its name. A robot the Channel's
creator adds gets 可获取所有内容，主动发送内容 (read everything, post on its own); one anyone else
adds can read and answer only what @s it. Give it the full permission (the Channel's creator
can change it in the Channel's robot settings): the Chat Bridge reads the whole post and its
replies and searches the Channel. In a direct message no setting is needed: find the robot
(its profile card has 发消息) and write to it.

**Set it up in Droi.** Settings → Chat Bridge: enter the Bot ID, the Secret and the Workspace
and Save. Turning it on asks to restart the Daemon once, which pre-approves the `droi-chat` tools so
their calls raise no Prompt. The page shows Connected, or WeCom's reason when it is not (a
wrong Secret shows WeCom's error 853000). Choose the Autonomy level (Autonomy 低 by default)
and the model Chat Sessions run with. On `droi host`, put the same settings in its settings
file (see [droi host on Linux](#droi-host-on-linux)). One robot keeps one long connection: a
second Host with the same robot takes it over, and the first one stops and says so.

**Who may talk to it.** Only the WeCom user ids on the allowlist start a Chat Session or answer
a Prompt; everyone else is ignored. WeCom gives a user's id in plain text (such as
`ZhangSan`) only when a super administrator created the robot; otherwise each company
sees an encrypted id. Rather than looking it up, have the person write to the robot once: their
id appears under Refused senders on the Chat Bridge page, and Allow moves it to the allowlist
(on `droi host`, move it from `refusedUsers` to `allowedUsers` and restart).

**Live test.** `TestLiveChatBridge` (`internal/chatbridge/live_test.go`) runs the Chat Bridge
against a real robot and a real Daemon through droid-proxy, and waits for a person: an @ of
the robot in a new post of the Channel `WECOM_CHANNEL` (default 定时任务) and then a direct
message, each asking for something that needs a permission request (such as running
`mkdir x` with Execute), answered in WeCom with `@robot 1` and with a card button. Its log
says what to do at each step. It is skipped unless `WECOM_BOT_ID`, `WECOM_SECRET` and
`FACTORY_API_KEY` are set; `WECOM_ALLOWED_USERS` (default: the Channel's creator),
`DROI_LIVE_WAIT` (10 minutes per step) and `DROI_LIVE_MODEL` are optional. It then records what
WeCom's docs leave open (token errors, emoji, file chunks and downloads, search) in its log,
and posts a few messages marked as a test.

```sh
export WECOM_BOT_ID=aib... WECOM_SECRET=...   # a test robot's
PATH=$HOME/.local/bin:$PATH FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) \
  FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') \
  go test ./apps/native/internal/chatbridge -run TestLiveChatBridge -v -count=1 -timeout 60m
```

## Following another computer

Settings → Computer shows which computer the window follows: this one, or one other computer,
added by pasting the pairing link a phone would scan (the link `droi host` prints, or the one
in Settings → Remote Access of another Droi). Droi checks that the link reaches a Droi Gateway
that accepts its Pairing Token, keeps it as a Client preference (`droi.computer` in
`preferences.json`) and relaunches onto it. The window then connects to that Gateway instead
of its own, lists that computer's Sessions and hides the Account, Memory, Remote Access and
Advanced settings, which belong to this computer's Host. This computer's Host keeps running
for its phones and Automations. "Use this computer" (also offered when the other computer
cannot be reached) clears the preference and relaunches.

Preferences that name Workspaces by path (pinned and folded Workspaces, their manual order, the
preferred Open in app) are kept per computer: this computer's stay under their old keys in
`preferences.json`, so an update reads them unchanged, and another computer's sit under the same
key with `@` and the computer id added. Drafts and Session stats are keyed by Session id and need
no split.

## Layout

| Path                                                            | What                                                                                                  |
| --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `main.go`                                                       | the app: window, menus, the Host                                                                      |
| `assets/`                                                       | the vendored Geist fonts, embedded                                                                    |
| `internal/theme/`                                               | the design tokens of `global.css`: the three themes, fonts, text sizes                                |
| `internal/icons/`                                               | the Lucide icons the app draws (`scripts/gen-icons.mjs`)                                              |
| `internal/host/`                                                | the Host: Shell settings, Daemon supervisor, Factory sign-in, the droid CLI's login                   |
| `internal/memory/`                                              | Memory's store (SQLite, the Electron app's schema), Memory Server, hook, Markdown export              |
| `internal/memorywork/`                                          | Memory Sessions: consolidation, extraction, prompts, what Settings → Memory shows                     |
| `internal/gateway/`                                             | the Gateway for paired phones: proxy to the Daemon, pairing link, Scratch and session-file endpoints  |
| `internal/computer/`                                            | the Computer preference: the one other computer the window follows, and checking its pairing link     |
| `internal/updates/`                                             | the in-app update: check, download, restart (MyGo's signed updates)                                   |
| `internal/prefs/`                                               | the Client's local preferences (localStorage's place)                                                 |
| `internal/transcript/`                                          | messages to transcript entries, tool calls, Script runs, json-render (daemon-layer port)              |
| `internal/sessions/`                                            | the Session list: grouping, sorting, pins, activity (daemon-layer port)                               |
| `internal/automations/`                                         | the Automations page's schedules (local time to the Daemon's UTC cron), labels and ids                |
| `internal/models/`, `defaults/`, `skills/`, `mcp/`, `slash/`, … | models, Session Defaults, skills, MCP, slash items and the rest of the daemon layer, one package each |
| `internal/md/`                                                  | streaming Markdown to blocks                                                                          |
| `internal/highlight/`                                           | code colours as Shiki gives them                                                                      |
| `internal/kit/`                                                 | the shadcn/Base UI look as MyGo elements: buttons, menus, selects, switches, dialogs                  |
| `internal/app/`                                                 | the views: shell, sidebar, Session, composer, prompts, settings                                       |
| `testdata/reference/`                                           | the web Client's screens at 2x with computed styles, recorded by `tests/web/native-reference.spec.ts` |

## Commands (from the repository root)

```sh
go run ./apps/native                 # run it (needs droid on PATH)
go test ./apps/native/...            # unit tests, view tests, Fake Daemon tests
go tool -modfile=apps/native/go.mod mygo build apps/native   # .app and .dmg
DROI_NATIVE_REF=1 pnpm test:e2e --project=desktop native-reference.spec.ts   # re-record the reference (deprecated web suite)
node apps/native/scripts/gen-icons.mjs [icon-name ...]   # add Lucide icons, or rewrite those in internal/icons/svg
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

`DROI_NATIVE_SHOTS=/tmp/shots go test ./apps/native/internal/app -v` writes each screen and a
diff (red: a channel off by more than 24 of 255) next to the share of pixels that differ.
Most of what is left is glyph edges, as MyGo and Chromium antialias text differently.

| Screen                                                       | Differs                   |
| ------------------------------------------------------------ | ------------------------- |
| session (light / dark / Solarized)                           | 2.0% / 1.9% / 2.0%        |
| session, tool call open                                      | 1.8%                      |
| sidebar hidden                                               | 1.8%                      |
| New session                                                  | 2.0%                      |
| Settings: General / Session defaults / Notifications / About | 1.7% / 2.5% / 2.0% / 2.0% |
| permission / AskUser                                         | 1.2% / 0.9%               |

Settings leaves out Account, Memory, Remote Access and Advanced when there is no Host (the
tests); the app has them all.

Live tests against a real Daemon, through droid-proxy, skip without a key:

```sh
FACTORY_API_KEY=$(grep -m1 '^fk-' ~/.config/dp/keys.txt) FACTORY_API_BASE_URL=$(dp status | awk '/baseURL/ {print $2}') \
  go test ./apps/native/internal/host ./apps/native/internal/gateway -run Live -v
```
