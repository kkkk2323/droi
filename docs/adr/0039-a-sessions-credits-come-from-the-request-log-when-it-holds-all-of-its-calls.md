---
status: accepted
---

# A Session's Credits come from the request log when it holds all of the Session's calls

The Session footer showed the Daemon's `factoryCredits`, which droid works out from the token
counts it reads off each response. Measured against Factory's billing counter (`chat-usage`'s
`userTokens`, read before and after one `droid exec` with a key nothing else used), that figure
missed what Factory bills in both directions:

- **Gemini**: droid puts the prompt count, which holds the cache reads, into its input tokens and
  then adds the cache reads again: Gemini 4 Carbon 383,855 reported against 75,505 billed, Argon
  397,197 against 76,170, the same with droid 1.9.0.
- **Anthropic**: cache writes kept for an hour (`cache_creation.ephemeral_1h_input_tokens`, which
  droid asks for on some calls) are billed at 2 input tokens each, droid prices them at 1.25: a
  Haiku 5.5 run 2,843 reported against 3,155 billed.

The Key Pool's proxy sees every response, and the request log keeps its tokens; priced at droid's
own weights (ADR 0038), with the hour-long writes at 2 and OpenAI's `cache_write_tokens`, which the
proxy did not read before, the log matched the counter within 5 credits for Gemini 4 Carbon,
GPT-6.1 Sol and Haiku 5.5 (`TestLiveRequestLogCreditsMatchFactoryBilling`).

We decided:

- **The footer shows the log's figure when the log holds every call behind the Daemon's.** droid
  reads the output, cache-read and cache-write tokens off the responses as the proxy does, for
  every provider; when the log's sums of the Session's calls (and its subagents', for the figure
  that includes them; the turn's time for the last turn) match the Daemon's within 1%, and every
  call is priced, the log's figure replaces the Daemon's.
- **Otherwise the Daemon's figure stays**: no Key Pool, a Session older than the log, or calls the
  proxy logged before it read a field (OpenAI's cache writes, for Sessions before this change).
- **The popover says which it is**, and what droid reports when that differs by over 1%.
- **No correction for a known droid bug.** 1.95.0's footer took droid's Gemini recount off its
  figure; that fixed one bug and would have missed the next one.

## Considered Options

- **The Daemon's figure, corrected for each bug found.** Every new bug in droid's accounting needs
  another rule, and nothing shows when one appears.
- **Factory's billing counter.** It is per key and per org, not per Session.

## Consequences

- On this computer's log, 659 of the 747 Sessions it has calls of are covered; the 20 of those whose
  figure moved by over 2% all used Gemini.
- How Droi prices a call is now what the footer shows: `TestLiveRequestLogCreditsMatchFactoryBilling`
  (with a key of the user's, real credits) is how to check it against Factory when a provider or
  droid changes.
- The footer reads the request log in memory, which the Observe page already kept: about 180 ms to
  read 92,000 calls once, a few milliseconds a lookup, again only when the Daemon's figures change.
