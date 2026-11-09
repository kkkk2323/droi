---
status: accepted
---

# Factory's models reach the Kernel through a Go proxy that keeps their request shape

With the droid Daemon gone (ADR 0038), nobody talks to Factory's API on Droi's behalf any more. pi-ai calls a provider itself, with the base URL and the key it was configured with, so Droi must supply both. Factory's endpoint is not a plain Anthropic or OpenAI endpoint: it expects droid's own first line in the prompt, rejects Pi's opening sentence, refuses some fixed phrases in tool output, wants a `X-Factory-Client: cli` header, serves Europe from another host, and answers `413` to a request whose attachments have grown — and none of that belongs in the Kernel, which must never hold a Factory key (ADR 0038).

Two ways to do the rewriting were on the table: wrap pi-ai's `fetch` inside the Kernel, or point pi-ai at a local proxy that rewrites on the way out. Wrapping fetch is what a spike did first, and it does not cover the fourth dialect: pi-ai's `google-generative-ai` adapter refuses a custom fetch, so Gemini models would have had to go through something else anyway. Three of four routes working is not a design.

We decided that **Factory is the Kernel's primary provider, reached through the Host's own LLM Proxy**, and that the proxy learns Factory's request shape:

- **`baseUrl` points at `pkg/llmproxy`, and nothing else does.** pi-ai is configured with one provider whose base URL is the Host's loopback proxy and whose key is a placeholder; the proxy swaps in the Key Pool's current key and answers in the dialect the model asked for. The `google-generative-ai` limitation stops mattering, because the Kernel's side of the wire is ours.
- **The key never leaves the Go process.** The Kernel holds a placeholder, the proxy holds the pool. This is the same rule as the Key Hook (ADR 0033) with a simpler mechanism: pi-ai asks `apiKey.resolve()` per request, which is a function Droi provides, and the proxy stamps the real header on the way out.
- **The rewriting layer is a Go port of the subset Droi needs from [`@magpie-community/opencode-factory-auth`](https://github.com/magpie-community/plugins/tree/main/packages/factory) 0.1.42 (MIT), read from the pinned copy at `shared/reference/magpie-factory/` (upstream commit `e7cb9dc54cd77850014cd8cc18b85b0fe1680fa2`).** Not a JS plugin running in the Kernel: a Go package in `pkg/llmproxy/factory/`, with the upstream file and line span at the top of each ported file and a `PORTED-FROM.md` naming version, commit and licence.

## What is ported, and what is not

Ported, about 1060 new lines of Go across 44 functions:

| Group | What it does |
|---|---|
| Identity | droid's first line added at the front of the system prompt, where Factory expects it |
| Pi's sentence | pi-ai's opening sentence and its compaction sentence rewritten wherever they appear, including inside tool output, escaping losslessly |
| Dialects | Anthropic Messages, Responses, Chat Completions and Gemini bodies, each shaped as droid shapes it: droid identity first, empty `system` dropped, a duplicate identity removed, Gemini's model moved into the body, `temperature`/`top_p` dropped for OpenAI reasoning models, `context_management` removed while streaming |
| Fast Mode | a base model plus `speed: fast` / `service_tier: priority` replaced by its Fast Mode sibling, with the Anthropic beta header |
| Organisation | the key's organisation and region from `whoami`, Europe served from `api.eu`, and one retry after a refresh when an organisation is refused |
| Too large | `413` answered by dropping the oldest attachments for the body's dialect and sending once more |
| Refusals | a `403` attributed to the organisation, the content policy or something else, with the upstream request id kept |
| Usage | the Standard 5-hour, 7-day and 30-day windows, Core and Extra, mapped onto what the Usage page shows |
| Models | the Factory model table with its multipliers and promotions, as data |
| Tool schema | the wrapper that lifts a root-level `anyOf`/`oneOf`/`allOf` out of an Anthropic tool schema, because Factory's Anthropic route refuses one, and the unwrapping of its streaming reply |

Not ported, 538 lines of the plugin plus 153 lines of account login: everything that only exists to fix Claude Code's prompt (its environment and skill notices, its `/compact` attachments, its quoted tool text), ZCode's context, and OpenCode's or the AI SDK's provider configuration. Droi controls its own prompt, and pi-ai does not send those shapes.

## Keys, and no logins

- **The Key Pool is the only Factory credential.** A pool entry is one `fk-` key and the day it was added; there is no device flow, no WorkOS token, no refresh, no JWT, and no "hand the login to the CLI". Users paste a key in Settings → Key Pool.
- **Rotation stands.** Automatic mode picks the next key when the current one runs out, Manual mode keeps the chosen one, and a key's Factory plan sizes its quota. A key's organisation and region are cached per key, as `pkg/llmproxy/keys/client_headers.go` already does.
- **`Host.HasCredential()` keeps its meaning, not its implementation.** It answered "is there a Factory login"; it now answers "is there at least one key in the pool". Its four callers do not change.
- **What is deleted, about 2309 lines, with no other caller:** `pkg/host/factory_auth.go` (504, the device flow and token refresh), `pkg/host/cli_login.go` (359, droid's `auth.v2.*` files and keychain), `pkg/host/accounts.go` (269, the per-account folders), `pkg/host/usage.go` (370, the account page's usage), `internal/app/signin.go` (174) and `internal/app/accounts_view.go` (633). ADRs 0005, 0015 and 0017 go with them.
- **Two proxy behaviours go too:** `proxy/prem.go`'s response rewriting, which existed so droid's session sync could pass, and `proxy/scriptmodels.go`'s `/api/feature-flags` rewrite, which existed so more models could run Script. Neither has a subject any more.

## Provenance and following upstream

Droi is betting on a third party's reverse engineering of Factory's API. The port therefore has three guards:

1. **Golden tests from upstream's own suite**, about 83 of its cases turned into Go table tests: Pi's sentence on three routes, droid's identity, Gemini's body, Fast Mode, `413`, multipliers, usage windows, the three `403` attributions, and the tool-schema wrapper. Five of its `testdata/` files come along, including droid's own model registry, so the model table is checked against droid's rather than against our reading of it.
2. **`pkg/llmproxy/factory/PORTED-FROM.md`** names version, commit, licence and what was left out, so the next reader can diff.
3. **A Droi Automation once a week** compares the upstream commit with the one in that file, filters the diff to the functions in the ported list, and writes a report into a Session. It only reads GitHub and sends no Factory request. The pinned copy in `shared/reference/magpie-factory/` is refreshed with the same pass, so the next person to read the plugin reads the version the port was checked against.

## Where to read the plugin before writing a request

The plugin's source is pinned beside this Workstream's other references: `shared/reference/magpie-factory/`, at upstream commit **`e7cb9dc54cd77850014cd8cc18b85b0fe1680fa2`** (written in that folder's `UPSTREAM_COMMIT`). It is a copy, not a checkout: it survives a reboot, a `/tmp` sweep and a fresh clone, and it is the file two people arguing about Factory's shape should both read.

Two rules come with it:

- **Check the plugin before writing a request by hand.** `README.md`'s Models table says which API each model takes; `index.mjs`'s `MODELS` (from about line 47) carries `wire` and `upstream` (sent as `x-api-provider`), the context window and the thinking levels; `droidBody` and `factoryHeaders` carry the request shape and the headers. A request assembled from memory is a request that is wrong in a way a `4xx` will not name.
- **Factory's error text is not evidence.** The same failure that a wrong route produces is reported as a region problem: in M1 a GLM model was sent to the Anthropic Messages route without `x-api-provider`, and Factory answered `Provider not available in this region`. That reads like an account or a region, and it was neither. An error is a hypothesis until the plugin's code and its `testdata/` agree with it; until then, a report says "unconfirmed".
## Considered options

- **Require droid-proxy (`dp`).** Rejected: it asks the user to install and run a second program, and it only moves traffic, it does not shape requests.
- **Run the JS plugin inside the Kernel.** Rejected: it needs the key in the Kernel, it is written against OpenCode's plugin API, and it would be a second runtime with its own supervisor in a process that already has one.
- **Wrap pi-ai's `fetch` in the Kernel and rewrite there.** Rejected: `google-generative-ai` refuses a custom fetch, so Gemini would need a proxy anyway, and the key would live in the Kernel.
- **Keep Factory accounts, with device flow, for the users who have no key.** Rejected by the user: device flow was never confirmed to yield a `fk-` key, and a login that Droi cannot turn into a key does not help the proxy.

## Consequences

- **A new user copies a key from Factory's website** instead of signing in. That is a worse first run, accepted deliberately.
- **No Factory Account settings page**, and no per-account Sessions, plan or renewal. Multiple accounts become multiple keys.
- **Droi now depends on a third party's reading of Factory's API**, kept current by the port's tests and the weekly Automation. When Factory changes a rule and upstream has not caught up, the fallback is a non-Factory provider or a direct key.
- **About 1060 lines of Go are new maintenance in this repository**, and they are the part most likely to be touched by a Factory change.
