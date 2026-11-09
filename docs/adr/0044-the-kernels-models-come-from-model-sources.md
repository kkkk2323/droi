---
status: accepted
---

# The Kernel's models come from Model Sources, and no Factory account is needed for them

ADR 0039 made Factory the Kernel's primary provider: the Host started the Key Pool's proxy and gave the Kernel Factory's models on it, and nothing else. Two things in daily use did not fit. A Magpie gateway (CONTEXT.md) was only ever handed to the Daemon, as droid's custom models in the Runtime Overlay, so a Kernel had none of its models even when Magpie ran on the same computer. And the window still asked for a Factory sign-in before it showed anything, though no Kernel request is made with that login: the Key Pool's keys carry Factory's, and Magpie needs none.

We decided:

- **A Model Source is one place the Kernel's models come from.** In the Host it is a small Go interface, `modelSource` in `pkg/host/model_sources.go`: one `load` that starts what the source needs and answers its pi-ai providers, how it stands (ready, off, empty, unreachable, with a model count and an error), and, when its requests carry the Kernel's token, the step that lets that token in. The Host loads every source for a Kernel launch and gives it all their providers together.
- **There are two sources.** Factory, through the Key Pool's proxy as ADR 0039 has it, whatever `FACTORY_API_BASE_URL` says (that variable is the Daemon's). And Magpie, when Settings → Advanced turns it on: one provider, `magpie`, on the gateway's OpenAI Chat Completions endpoint, each model under the gateway's own id, with the context window, output limit, image input and reasoning efforts the gateway lists. Its key is the fixed one a loopback client sends, set in every launch's environment, so Magpie turned on later reaches a running Kernel without a restart.
- **A change reaches the running Kernel.** Whenever a source may have changed — the Key Pool changed, Settings changed, Magpie's list was read — the Host loads the sources again and, when the providers differ from what the Kernel has, gives them with `harness.configure`. Runs keep going; only a Kernel that refuses the change is started again. This replaces the Key Pool's own check (the proxy moved, the first key came).
- **The window says where models come from.** Settings → Advanced lists each source and how it stands. When the Kernel has no model, the New session page says, for each source, what would give it some: a key on the Usage tab, Magpie turned on, Magpie started. Droi's own Factory sign-in page and the Account tab are not shown on the Kernel; an app's own sign-in page (ADR 0041) still is.

## Considered options

- **Plugins in the Kernel, Magpie's way, each source a JS module pi-ai loads.** Rejected for now: Factory's source needs the Go proxy anyway (ADR 0039), and the Host is where the settings, the Key Pool and the hot reconfigure already are. The interface is shaped so that a source the user adds later is one more implementation in the Host; how such a source is described and loaded is left until there is one.
- **Hand Magpie's models to the Kernel through the Factory proxy.** Rejected: Magpie is on this computer and takes any key from it, so the proxy would only add a hop and Factory's request rewriting, which is wrong for Magpie's models.
- **Keep the sign-in page until the Daemon goes.** Rejected: on the Kernel it blocks the window for a login nothing uses, and the user who has only Magpie, or only Key Pool keys, cannot get past it.

## Consequences

- With no Factory key and no Factory login, Magpie alone runs a conversation.
- A Magpie model id has no `custom:` prefix on the Kernel: the Kernel names a model by provider and id, so the gateway's id cannot collide with Factory's. A Daemon Session's Magpie model id (`custom:magpie-…`) is not the Kernel's.
- Turning Magpie off removes its models from the running Kernel; a conversation that used one fails on its next turn until another model is picked.
- The Daemon's Magpie path (the Runtime Overlay) stays as long as the Daemon does.
