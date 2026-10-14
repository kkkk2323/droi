---
status: accepted
---

# The Chat Bridge in the Host runs Chat Sessions for WeCom Chat Threads

Users wanted to hand Droid work from WeCom (企业微信), in WeCom Channels and in direct messages with a robot, the way Factory's Slack Delegations work, but on a computer of their own. Factory's Slack and webhook triggers run in Factory's cloud, which ADR 0016 left out of Droi.

We decided to build the Chat Bridge into the Host. It keeps the one long connection (WebSocket) WeCom offers an API-mode smart robot, so it needs no public address, and runs wherever a Host runs: the app or `droi host` (ADR 0019), off until its settings name a robot. Like Memory Sessions it dials the Daemon directly and is not a Client, so it adds the System Prompt Addition itself. Each Chat Thread gets one Chat Session in one fixed Workspace; Chat Sessions may run at the same time. A Chat Session carries the `droi.chat` tag with its Chat Thread in the tag's metadata, so the Daemon, not the Host, keeps which Session belongs to which Chat Thread. Clients list Chat Sessions with the others, and a Prompt is answered by whoever answers first, in WeCom or in a Client (ADR 0003).

The Chat Bridge is a trigger and a set of tools, not a renderer. When someone @s the robot it sends the Chat Session one message (who, where, and the text, truncated) and nothing more; while Droid is working on that Session it sends only a notice that the Chat Thread has unread messages, and Droid reads them when it chooses. Droid reads the Chat Thread, replies, and reacts through tools the Chat Bridge serves to that Session alone (a loopback MCP endpoint passed in the Session's `mcpServers`, as the TypeScript SDK serves its in-process tools); it decides how much to read and when and how often to reply, and sends its final answer itself. Only Prompts stay with the Chat Bridge, since the Daemon raises them before a tool runs.

WeCom delivers a Channel post twice: as a Channel event (`topic_at_bot`, with the Channel, post and reply ids, but no text) and, for robots written for group chats, again as a group-chat message with a per-post `chatid`. The Chat Bridge answers the Channel event: it reads the post and replies through the Channel's HTTPS API (`get_post_list`, `get_reply_list`, `send_reply`) and ignores the group-chat copy. Direct messages arrive as messages on the long connection and are answered there, with template cards for Prompts.

## Considered Options

- **A Daemon plugin.** Droid plugins carry skills, commands, droids, output styles, hooks and MCP servers, all of which run inside a Session; none can hold a connection open and start a Session.
- **A Droi plugin.** Droi has no plugin mechanism, and Go's `plugin` package needs cgo, which MyGo's builds turn off, and does not run on Windows.
- **A separate program that pairs with the Gateway as a Remote Client.** Leaves Droi untouched, but is a second program to install, configure, run and update beside the Host on the same computer.
- **The group-chat copy of Channel posts.** It streams and shows a reply under the post, but WeCom asks robots to move to the Channel API and is building a call that stops the copy; template cards sent through it show as "[不支持的内容]" in a Channel anyway.
- **WeCom's URL-callback mode, or a Factory webhook Automation.** Both need a public address (the second also a relay that verifies and decrypts WeCom's callbacks), and the Automation runs on Factory's cloud computers.

## Consequences

- ADR 0016's "Droi leaves Slack and webhook kinds out" no longer holds for WeCom; Factory's own Slack stays Factory's.
- Nothing streams: every reply is a message Droid chose to send. A turn in which Droid calls no tool leaves the Chat Thread silent.
- In a Channel, Prompts are answered by replying with an option's number, since Channel replies carry no template cards.
- WeCom's rate limits reach Droid as tool errors; the Chat Bridge does not queue for it.
- WeCom keeps one long connection per robot: a second Host started with the same robot disconnects the first.
- The Channel API is still in gray release (WeCom 5.0.12); a robot written for group chats also gets every Channel post through the group-chat copy until WeCom ships the call that stops it.
- Chat Sessions sharing the Workspace can edit the same files at the same time.
- The Daemon sends a Session's Prompts only to the Clients attached to it (ADR 0003) and tells no other connection that a Session's turn began, while the Chat Bridge is attached to a Chat Session only from a message until the Session is idle. A Prompt raised while only a Client has the Chat Session loaded (opened after the idle close, or after the Host restarted) shows in that Client alone; the Chat Thread sees it when its next message loads the Chat Session, since the Daemon returns a Session's pending Prompts with the load. The Fake Daemon sends Prompts to every connection, so the tests cannot show this.
- A `droi host` takes the Chat Bridge settings from its settings file; the window hides them while it follows another computer (ADR 0019).
