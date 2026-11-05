---
status: accepted
---

# Remote Access also goes through a Relay the Host dials out to, and a phone tries the network first

A phone reaches its computer's Gateway by the address in the pairing link: a LAN address, which works only at home, or a name that also reaches it away from home (`pairingHost`, ADR 0019), such as a Surge Ponte or Tailscale name. In practice the user ran Surge Ponte, and its log showed why the phone so often said "Can't reach": in three days Ponte failed 132 times to write the Mac's address to iCloud and 86 times to open its channel ("network unavailable", "no outgoing interface"), mostly after the Mac woke or changed networks. Ponte is UDP over a relay in China, so evening packet loss adds to that. The Gateway also kept listening only on the LAN addresses it found when Remote Access was turned on, so after the Mac moved to another network a phone on that network could not reach it either.

We decided:

- **A Relay is a server both sides dial out to.** The Host keeps one WebSocket open to it (`wss://relay/agent`); a phone sends its requests to `https://relay/c/<Relay Id>/...`, and the Relay passes them, and WebSockets, through that socket to the Host, which answers them from its own Gateway over loopback. TCP and TLS on port 443 only: no UDP, no iCloud, no port forwarding, no VPN on the phone. The Host redials on its own after a sleep, a network change or a Relay restart.
- **The protocol is alma-relay's** (github.com/yetone/alma-relay, `docs/PROTOCOL.md`, protocol 2): the same frames, binary payloads, streamed request bodies with a credit window, and close codes. Droi's own Relay is `droi-relay`, written in Go beside the Gateway but a program of its own for a Linux server: a 3.5 MB download where `droi` is 11 MB (in `droi` the server would add only 540 KB; the point is what the server downloads and a help that is only about the Relay). The Host's side also works against alma-relay's Node and Docker servers. Droi leaves out what Alma adds for itself (APNs and FCM pushes, TURN, signed per-user tokens).
- **The Relay Id is not the Pairing Token.** Each Host has a random Relay Id, kept in its settings; the Host registers with its SHA-256, and only a phone that knows the Id can address the Host. The Pairing Token still authorizes every request at the Gateway, exactly as on the network. A Relay may require a registration key, so that strangers cannot use it to reach their own computers.
- **A Relay has a domain name, and installs with one command.** `install-relay.sh` downloads the latest `droi-relay` and runs `droi-relay install --domain <name>`: it makes the registration key, gets a certificate from Let's Encrypt by tls-alpn-01 on port 443, installs a systemd service and prints the **relay address**, `https://<name>/#key=<key>`. That one address is what the user pastes in Settings → Remote Access, which walks through the three steps. The key rides in the fragment and stays on the computer; the pairing link carries the Relay without it. The certificate is asked for once: a closed port or a wrong DNS record fails at once with what to check, instead of a service retrying until Let's Encrypt's few failures an hour are spent. Behind Caddy or nginx, `--listen` and `--public-url` replace `--domain`.
- **The pairing link carries both ways.** `http://<LAN address>:<port>/#pair=<token>&relay=<https://relay/c/<Relay Id>>`, or, with no LAN address, the Relay address alone. A Client keeps both. On every connect, and whenever the Phone App comes back to the foreground or loses its socket, it asks the network address first and uses it when it answers within a moment; otherwise it uses the Relay. At home the traffic stays on the network.
- **The Gateway's `/meta` names the Relay**, `""` without one, and a phone keeps what it names whenever it connects. A phone paired before the Relay was set, or before it changed, follows it without scanning again.
- **The pairing address is gone.** `pairingHost` (ADR 0019) put a name that reaches the computer from away into the link, for Surge Ponte or Tailscale; the Relay replaces it. A phone can still change a computer's address by hand.
- **Every Client builds its Gateway URLs on a base that may have a path** (`/c/<Relay Id>`), not on an origin.
- **The Gateway follows the network.** While Remote Access is on it checks the interfaces every few seconds, listens on new addresses, lets go of gone ones, and tells the Host, which then redials the Relay at once.

## Considered Options

- **Keep Surge Ponte.** Free and already set up, but its failures are in Surge and iCloud, out of Droi's reach.
- **Tailscale.** Reliable, but on an iPhone it is a VPN, and only one VPN runs at a time, so it cannot run beside Surge; in China it relays through DERP servers abroad.
- **frp or another generic tunnel, outside Droi.** Works, but it is one more program to run and watch on the Mac, the pairing link would not know about it, and the phone could not fall back to the network.
- **alma-relay as it is, Node only.** Its protocol is what we take; writing the server in Go keeps one language, shares the Gateway's code and lets `go test` cover the whole path. A user can still run alma-relay's server.
- **`droi relay`, a subcommand of the app's program.** One program to release, but a server downloads all of Droi for it, and its help and settings are the app's.
- **No domain: a certificate for the server's IP address.** Let's Encrypt issues them since January 2026, valid six days. It would spare buying a domain, but Go's autocert does not ask for them yet, and the six days leave little time when renewal fails. A domain is required for now.
- **A Cloudflare Worker.** Free to run, but `*.workers.dev` is blocked in mainland China, and a custom domain on Cloudflare is slow there.

## Consequences

- The Relay sees the requests in clear inside its TLS, Pairing Token included: whoever runs it could drive the computer. Run your own, over HTTPS. End-to-end encryption between phone and Host, as Alma has, is the next step and changes the link format.
- Remote Access must be on for the Host to dial the Relay; turning it off, or resetting the Pairing Token, ends the Relay's bridges as it ends the network's.
- A Relay needs a server reachable from both sides; one in Hong Kong is close to mainland China and needs no ICP filing.
- A sleeping computer is still unreachable; a Relay does not wake it.
- A phone paired before the Relay learns it the next time it connects to the computer, on its network; one that never does pairs again.
- The registration key is in the relay address, so whoever has that address can register a computer on the Relay; `droi-relay reset-key` makes a new one.
