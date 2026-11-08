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

`droidbody.go` opens a request's system prompt with droid's line. Factory's
usage policy check refuses a request that says it is something else with 403
`content_policy_rejected`, which is what the Kernel's own prompt got before
this layer existed (M1 acceptance, 2026-11-08).

| Go                                     | upstream (`index.mjs`)                     | notes                                                                                                                 |
| -------------------------------------- | ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| `DroidBody`, the route switch          | `droidBody` 613-651                        | only the identity half: no `promptOpenings`, no `temperature`/`top_p` removal for OpenAI models                       |
| `chatBody`, `droidChat`, `openContent` | `droidChat` 581-611                        | a chat completions first system message, string or parts                                                              |
| `responsesBody`                        | `droidBody`'s `/responses` branch, 630-644 | string `instructions` only; the input is left as it came                                                              |
| `anthropicBody`                        | `anthropicBody` 1609-1674                  | system blocks only: empty blocks go, the line is first, duplicate lines go; no billing header, no Claude Code context |
| `geminiBody`                           | `geminiBody` 663-698                       | the system instruction only, its role removed; the model in the body and the `/generate` path are not here            |
| `DroidLine`                            | `DROID_LINE` 470                           | the same sentence, byte for byte                                                                                      |

4A adds what a request needs around its body when the client holds no key (the
Kernel, decision Q15): the proxy picks a Key Pool key and sends the request as
droid would.

| Go                             | upstream (`index.mjs`)                               | notes                                                                                                                  |
| ------------------------------ | ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `models.go`                    | `MODELS` 59-116, `byId` 118, wires 33-39             | id, wire and vendor only; names, limits, efforts and prices come with the picker (4F)                                  |
| `headers.go`: `Headers`        | `factoryHeaders` 342-349, `send`'s headers 2071-2092 | the Authorization is the proxy's (the pool key); `X-Session-Id` is per conversation (`SessionID`), not one per process |
| `headers.go`: `ClientVersion`  | `VERSION` 23                                         | the same release, a constant: there is no droid to ask any more                                                        |
| `identity.go`: `LLMBase`       | `base` 329, `llmBase` 333-339                        | a base that is not Factory's own API (a test server, a proxy in front) is kept                                         |
| `identity.go`: `OrgRefused`    | `orgRefused` 405                                     | the proxy asks whoami again and sends once more with the same key                                                      |
| `explain.go`: `ContentRefused` | `contentRefused` 412-422                             | only to keep a content refusal from being blamed on the key; the explanation itself is 4D                              |

`route_test.go` checks every model id against the excerpts of droid's registry
upstream keeps in `testdata/` (copied byte for byte).

The golden cases in `droidbody_test.go` come from the upstream tests
`droid.test.mjs` (the cases that cover the routes Droi sends), which are
themselves the built-in's `TestFactoryOpensAsDroid`.

## What comes later

4B and 4C of the M4 plan (`shared/design/phased-plan.md` §6) add the rest of
the upstream layer to this package: pi's opening and compaction sentences,
`escapeDeep` and the refusal phrase list, `fastBody`, the OpenAI `temperature`
rule, the whole four-dialect body shaping, the 413 attachment drop and the 403
explanations. Model display names, limits and prices come with 4F. Those functions and their golden tests are listed in
`shared/design/gap-analysis.md` §6.

## Deliberate differences

- **A body that needs no change comes back byte for byte.** The reference
  re-serializes a Gemini body even when it only deletes a key, and a Responses
  body whose instructions already open with the line; here the original bytes
  are returned, so droid's own requests keep their exact bytes (and prompt
  cache).
- **A content array whose first part already opens with the line is left
  alone.** The reference prepends the line to any array that holds a part that
  is not text, so a request with an image first would collect one line per
  turn.
- **A system prompt of a shape droid does not send is left as it came**,
  rather than guessed at: a non-string `instructions`, a `system` block whose
  `text` is not a string, a Gemini `parts` that is not an array.

## Keeping up

`shared/design/architecture-draft.md` §5.8 describes the weekly Automation that
compares the upstream commit with this file's and filters the diff to the
functions Droi ports.
