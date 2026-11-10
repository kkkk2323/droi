# Daemon-reported Credits and request estimates

## Decision

Show `tokenUsage.factoryCredits` from the Daemon in the Session footer.
Use the inclusive counter when it has a credit field, otherwise the Session's
own counter. The popover separates both figures. Missing is not zero.

Show `agent_turn_completed.tokenUsage.factoryCredits` as the last **observed**
turn, with its observation time and a separate subagent figure when present.
Replace this snapshot, never add repeated notifications or subtract Session
counters. It lasts only while the view is kept; Session totals load again from
the Daemon. It is not a record of every historical turn.

The existing SDK carries these fields. No SDK, Fake Daemon, Key Hook, or billing
behavior changes are needed. This works without the Key Pool and when following
another computer.

## Request estimates

Keep Observability's filtered-period totals, groups, and request rows based on
the request log. Never overwrite them with a lifetime Session counter: the
periods, side calls, keys, and cache-warming traffic do not have the same scope.
Name the summary and detail figures “Estimated credits” and expose the source
and exclusion of promotions.

`daemon.list_models` exposes `tokenMultiplier`, but not separate output or
cache weights. A multiplier alone is not a full price. Do not infer a new
model's full weights from its family or spread Session credits across requests.

Fix the reported Fable 5.2 requests with the weights verified in the installed
Droid **1.7.0** sources: `model-registry.ts` gives a 3.2 input multiplier and
5x output, `usage/index.ts` applies the default 0.1x cache-read multiplier from
`common/usage/constants.ts`. The weights are **3.2 / 16 / 0.32**, not Fable 5.1's
cache rate. Existing saved token records can therefore be estimated.
These are CLI credit weights, not the registry's separate provider USD quotes.
The application does not decompile or inspect a binary at run time.

Match a known ID or a numeric dated suffix only. Unknown versions and fast
variants stay unpriced instead of borrowing another model's rate.

## Validation

- Reproduce the original unpriced request and the absent Daemon readout first.
- Test own/inclusive counters, missing fields, reported zero, invalid values,
  mixed-model-safe totals, turn replacement, and duplicate notifications.
- Exercise the footer and popover against the Fake Daemon in both themes.
- Check historical Fable requests, cache weights, unknown variants, and source
  text. Run workspace checks, native tests, and platform vet checks.
