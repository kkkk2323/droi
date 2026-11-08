# Ported from @magpie-community/opencode-factory-auth

```text
upstream:   magpie-community/plugins
package:    packages/factory
version:    0.1.42
license:    MIT
commit:     e7cb9dc54cd77850014cd8cc18b85b0fe1680fa2
reference:  shared/reference/magpie-factory/ (a copy of that commit)
ported:     2026-11-08
```

This package is Droi's rewrite layer for Factory: the part of that plugin Droi
needs, written in Go and run inside the LLM proxy (`pkg/llmproxy/proxy`) rather
than as a JavaScript OpenCode plugin. The design is
`shared/design/architecture-draft.md` §5.

## What is here now

`factory.Request(path, body)` is the way in. A Gemini request in Google's own
shape moves to Factory's `/generate` route with the model in its body; the body
takes droid's shape for its route; last, every refused phrase still in it is
escaped (`sanitizeBody`). Factory's usage policy check refuses, with 403
`content_policy_rejected`, a request whose system prompt does not open with
droid's line (M1 acceptance, 2026-11-08) and one that holds another client's
fixed phrases, pi's compaction opening among them (yetone/magpie#952, #1316).

| Go                                     | upstream (`index.mjs`, first line)                                                                             | notes                                                                                                                                                           |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Request`, the route switch            | `GEMINI_CALL` 661, `droidBody` 617, `send`'s Gemini path 2059                                                  | `fastBody` is 4C, the Anthropic tool schema wrapping 4E                                                                                                         |
| `chatBody`, `droidChat`, `openContent` | `droidChat` 583                                                                                                | a chat completions first system message, string or parts                                                                                                        |
| `responsesBody`                        | `droidBody`'s `/responses` branch, 630                                                                         | instructions; an OpenAI model's `temperature` and `top_p` go (633); the input gets `promptOpenings`                                                             |
| `anthropicBody`                        | `anthropicBody` 1609                                                                                           | system blocks, pi's opening and summaries; `context_management` off a streamed request (1738)                                                                   |
| `geminiBody`                           | `geminiBody` 669                                                                                               | the model in the body, no `stream`, no role on the system instruction; refused phrases escaped (699)                                                            |
| `openings.go`                          | `PI_OPENING`/`PI_COMPAT`/`clientOpening` 478, `PI_SUMMARIES`/`piSummary` 494, `promptOpenings` 554             | pi's opening, its compaction and branch summaries, tool output                                                                                                  |
| `escape.go`                            | `QUOTED_TOOL_PREFIX` 1447, `quotedToolText` 1459, `escapePhrases` 1474, `escapeDeep` 1490, `sanitizeBody` 1518 | tool output quoted as a JSON string, a `\u` escape for the first character elsewhere                                                                            |
| `phrases.go`                           | `client-phrases.json`, `FALLBACK_PHRASES` 515, `REFUSED_PHRASES` 1449, `ESCAPE_IN_TOOL` 1457                   | the strings are written with `\u` escapes, so reading this source cannot trip the check                                                                         |
| `fast.go`: `fastBody`, `FastBeta`      | `FAST_BETA` 125, `FAST_OF` 126, `fastBody` 135, `send`'s beta 2077                                             | runs before the dialect shaping, as upstream's; the beta is set with the other headers (`Headers`)                                                              |
| `DroidLine`                            | `DROID_LINE` 470                                                                                               | the same sentence, byte for byte                                                                                                                                |
| `json.go`                              | `JSON.parse`/`JSON.stringify` of each step                                                                     | a body that changes goes out as `JSON.stringify` writes it: keys in their order, array indexes first, new keys last, strings escaped as JavaScript escapes them |

A body that needs no change comes back as the very bytes it came as, as
upstream's does. One that changes goes out byte for byte as upstream sends it:
`testdata/parity/fixture.json` holds the requests pi-ai sends for the models the
Host registers, a few written by hand, and what upstream's plugin sends Factory
for each, signed in with a key as the Key Pool is (and again after a 413);
`proxy`'s `TestKernelRequestsGoOutAsUpstreamSendsThem` sends each through the
proxy and wants the same path, headers and bytes. To make the fixture again, from
`testdata/parity` with apps/kernel's dependencies installed:

```sh
DROI_PARITY_PROVIDERS=$PWD/providers.json go test ../../../../host -run TestKernelProvidersOfTheParityFixture
bun run requests.mjs
UPSTREAM=<magpie-community/plugins at the commit above>/packages/factory bun run upstream.mjs
```

4A added what a request needs around its body when the client holds no key (the
Kernel, decision Q15): the proxy picks a Key Pool key and sends the request as
droid would.

| Go                            | upstream (`index.mjs`)                               | notes                                                                                                                                                                                  |
| ----------------------------- | ---------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `models.go`                   | `MODELS` 59-116, `byId` 118, wires 33-39             | name, wire, vendor, limits, efforts, images and token multiplier; the Host hands them to the Kernel (`kernelProviders`)                                                                |
| `headers.go`: `Headers`       | `factoryHeaders` 342-349, `send`'s headers 2071-2092 | the Authorization is the proxy's (the pool key), with no `X-Factory-Org-Id` (a key account's, `keyAccount` 824); `X-Session-Id` is per conversation (`SessionID`), not one per process |
| `headers.go`: `ClientVersion` | `VERSION` 23                                         | the same release, a constant: there is no droid to ask any more                                                                                                                        |
| `identity.go`: `LLMBase`      | `base` 329, `llmBase` 333-339                        | a base that is not Factory's own API (a test server, a proxy in front) is kept                                                                                                         |

| `explain.go`: `ContentRefused` | `contentRefused` 412-422 | keeps a content refusal from being blamed on the key |

4D added what the Kernel is told when Factory turns a request away for what it
says or for its size, rather than for the key.

| Go                                                                           | upstream (`index.mjs`)                                                                                            | notes                                                                                    |
| ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `attachments.go`: `LeaveOutAttachments`, `TooLarge`                          | `KEEP_ATTACHMENTS`/`LEFT_OUT`/`TOO_LARGE_HINT` 717-722, `DIALECTS` 734, `leaveOutAttachments` 744, `tooLarge` 782 | the proxy asks once more on a 413 (`proxy/kernel.go`), as upstream's `smaller` 2119 does |
| `explain.go`: `ContentPolicyHint`, `VendorError`, `UpstreamIDs`, `ErrorBody` | `explain` 428 (its content refusal), `vendorError` 952, `upstreamIds` 436, `errorReply` 792                       | a content refusal goes back as a 400 with its code, as upstream's 2145-2153              |

4F added a model's price now.

| Go                  | upstream (`index.mjs`)                 | notes                                                                                                        |
| ------------------- | -------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `price.go`: `Price` | `PROMOTIONS` 173-179, `rateOf` 186-190 | every promotion of droid 0.241.1's registry, ended ones too, so the price agrees with droid's at any instant |

Upstream's `PROMOTIONS` lists five promotions; droid's registry (`testdata/droid-0.241.1-costs.txt`) has more, `M` on the GPT and Grok models among them. `price.go` follows droid's registry: do not drop those to match upstream's list.

4G added how a usage window's end is read.

| Go                      | upstream (`index.mjs`) | notes                                                                                               |
| ----------------------- | ---------------------- | --------------------------------------------------------------------------------------------------- |
| `usage.go`: `WindowEnd` | `windowEnd` 971-981    | the Key Pool (`keys.BucketLimit`) and the account's Usage tab (`host.parseUsage`) both read with it |

The windows themselves were already read by the Host for the Usage tab
(`pkg/host/usage.go`, Standard and Core, the extra usage in cents);
`limitWindows`' routing shape (`aside`, `models`, `notModels`) is magpie's and is
not ported.

`route_test.go` checks every model id against the excerpts of droid's registry
upstream keeps in `testdata/` (copied byte for byte; `client-phrases.json` is
copied the same way for `TestThePhrasesAreUpstreams`).

## Golden cases

The subtests keep the upstream names, so a diff of upstream's tests finds them.

| Go                      | upstream                                                                                                                                                     |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `openings_test.go`      | `pi.test.mjs`, all 12                                                                                                                                        |
| `gemini_test.go`        | `gemini.test.mjs`, 4 of 5 (the EU route is the proxy's `LLMBase` test)                                                                                       |
| `escape_test.go`        | `request.test.mjs`'s three tool output cases (422, 450, 485); the phrase list against `client-phrases.json`                                                  |
| `shape_request_test.go` | `droid.test.mjs`'s `temperature`/`top_p` (198) and 1017 (83) cases; `request.test.mjs` 168 and 204                                                           |
| `droidbody_test.go`     | `droid.test.mjs`'s opening cases, which are the built-in's `TestFactoryOpensAsDroid`                                                                         |
| `fast_test.go`          | `fast.test.mjs`, 3 of 4 whole; the fourth's order check (its first-24 check is magpie's picker)                                                              |
| `attachments_test.go`   | `size.test.mjs`'s fourth case; `content-policy.test.mjs`'s last (`contentRefused`)                                                                           |
| `rate_test.go`          | `rate.test.mjs`'s first three, and every model at each promotion's start and end (its last two are magpie's provider hook)                                   |
| `usage_test.go`         | `usage.test.mjs`'s window ends (its first case's three spellings, and the second's "soon"); its account and sign-in cases are the Host's own `usage_test.go` |
| `proxy/kernel_test.go`  | `size.test.mjs`'s first three; `content-policy.test.mjs`'s first three and the upstream-error-header case                                                    |
| `request_test.go`       | `request.test.mjs` 72, 94, 151, 159, 193 and 212; `droid.test.mjs` 50, 100, 114 and 123; `effort.test.mjs`'s second; `models.test.mjs` 36, 89 and 115        |
| `proxy/parity_test.go`  | upstream's plugin itself, on the requests of `testdata/parity` (above)                                                                                       |

`content-policy.test.mjs`'s whoami case is not ported: the Key Pool tries
another key for a 403 that is not a content refusal instead.

`shape_request_test.go`'s `context_management` case has no upstream test; it
holds `anthropicBody`'s lines 1738-1743.

## Deliberate differences

- **No Anthropic tool schema wrapping here** (`anthropic-tools.mjs`,
  `schema.test.mjs`). pi-ai's Anthropic adapter sends a tool's root
  `properties` and `required` only, so a root `anyOf` never reaches the proxy
  to be wrapped; and on the OpenAI routes it arrives unwrapped and is
  refused. The Kernel wraps such an MCP tool's schema when it offers the tool
  (`apps/kernel/src/mcp/wrap.ts`, its `$ref` relocation ported from
  `anthropic-tools.mjs`), which serves every route. droid's own requests never
  had the problem.

- **Numbers keep their spelling.** A number goes out as it came. Upstream
  writes `1.0e3` as `1000`, and on the OpenAI and Gemini routes rounds an
  integer past 2^53; pi-ai writes every number as JavaScript does, so the
  Kernel's requests go out the same either way.
- **Only the content refusal is explained to the Kernel.** Another 401 or
  403 makes the Key Pool try another key; when none is left the Kernel gets
  the Key Pool's one sentence, and Factory's own answer stays in the proxy's
  record. Upstream has one account and explains each 403 instead.
- **The 413 hint names no client's command.** Upstream's says `/compact in
Codex or Claude Code`, for magpie's agents.
- **Droi's own headers and the Kernel's token stay in the proxy.** Every
  `X-Droi-` header, and a Gemini request's `key` query, go nowhere.

## Keeping up

`shared/design/architecture-draft.md` §5.8 describes the weekly Automation that
compares the upstream commit with this file's and filters the diff to the
functions Droi ports.
