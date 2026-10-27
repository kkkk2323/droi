---
status: superseded by ADR-0031
---

# Global Memory has a core and topics

_Superseded by ADR 0031: Memory is one folder of Session Summaries, Memory Notes and a Memory Summary, with no Global Memory, core or topics._

Global Memory filled up with facts that are not about the user. On 2026-10-26 one heavy user's had 134 entries in 39k of its 40k characters; a consolidation took it from 41k to 39k. About half the entries were about one subject each (a company's cloud accounts, a home network, a hobby), saved from chats without a project, which have no Project Memory to write to. ADR 0010's consolidation could only move such an entry to a Project Memory that already existed, or remove it, and its prompt said to keep an entry when unsure, so it kept them. It also compared entries only within one category, so the same fact saved as a preference and as a correction survived. Worse, the user's 23 Global corrections took 5.5k characters, and the SessionStart hook injected the newest within 2,000 characters shared with the Project Memory: 8 of them reached a Session, and the other 15 never did.

The tools we compared all keep a small layer in every context and a larger one found on demand: Hermes Agent injects 2,200 + 1,375 characters and refuses a write past them; Claude Code loads the first 200 lines of a `MEMORY.md` index and reads topic files when needed; ChatGPT moves saved memories it judges less important to the background instead of deleting them; Letta keeps 2k-character core blocks in context and searches archival memory by tag; mem0 tags every memory with a category to filter by.

We give the Global Memory two layers, without moving anything out of it:

- **The core** is the entries without a topic: facts about the user that hold in every Workspace (how they like to work and be answered, their computers, accounts and everyday tools). Its corrections are injected at SessionStart within 3,000 characters of their own; the Project Memory's keep their 2,000.
- **A Memory Topic** is one subject outside any project, such as a service the user runs, a client or a hobby. Its entries stay in the Global Memory, so every Session can search them, but none is injected: SessionStart names the topics and their sizes in one line of at most 600 characters, and the policy says to list a topic before work that touches it. A topic's corrections are listed first.

A topic is a label, not a slot: `entry_topics (entry, topic)` beside `entries`, so a database from before has no migration to run and an older Droi ignores the table (its foreign key still drops a removed entry's topic). Only Global Memory entries have one.

- `memory_add` takes a `topic` for a global entry and answers a new topic with the ones that exist, so the same subject does not get two names. `memory_search` and `memory_list` filter by topic, and `memory_replace` can change only the topic. A write that takes the core's corrections past 3,000 characters is saved, never refused (a lost correction is the failure the user notices), and answered with the core's corrections and a request to shorten, merge or file them.
- A chat without a project may now save a durable fact about what it is about, with a topic; it still leaves out one-off details.
- Consolidating the Global Memory sends its core, every category together, then each topic of two or more entries, in slices of 15k characters rather than 40k: on the user's data, gemini-3.8-flash given 40k characters of mixed entries thought for six minutes and answered nothing. Every Memory Session now asks for low reasoning effort, which the Daemon replaces with the model's default where the model has no low: that model defaults to high, and spent five minutes on 33 entries even so. The answer can file a core entry under a topic (a new one or one of those listed) or put a topic's entry back in the core, beside keep, rewrite, remove, merge (now with the merged entry's category) and move. When unsure whether an entry holds in every Workspace, the prompt now files it rather than keeping it in the core. Each entry is sent with how many searches found it. A move now has a topic as the safer choice beside it, and is kept to entries that name the project: a move of a correction or a preference, or of an entry whose words do not include the project folder's name, is applied as a keep. On the user's data the model had moved a rule against kubectl on production, and git and CI quirks, to whichever project they were saved in. A Global Memory never sorted is sorted before it is consolidated. Project Memory consolidation is unchanged.
- The first time Memory work runs after the upgrade, a sort files the core into topics once, 8k characters at a time, each slice seeing the topics the ones before it named, from a prompt of its own that changes no text and removes nothing, so an edited consolidation prompt does not stop it. A sort whose every answer is rejected is tried again at the next launch. Settings → Memory shows the core's size, its corrections against their budget and the largest topics; a corner card says once how many entries the sort filed.
- Before a Global Memory consolidation or the sort, the database is copied to `memory.backup-<day>.sqlite`, once a day.

## Considered Options

- **Move topic facts to a Project Memory.** Rejected as the only way: many have no project, and a Project Memory is not read outside its Workspace, while a subject such as the user's cloud accounts comes up in chats and in several projects.
- **A Memory keyed by topic name instead of by Workspace.** Rejected: no Session would load it on its own, every tool would need a third scope, and moving entries in and out is a move between slots, where a label change is one row.
- **Remove more.** Rejected: the entries are true and some are found often; repeated rewrites lose rules (the consolidation audit of 2026-10-03), and ChatGPT and Claude Code both demote rather than delete.
- **Refuse a correction past the budget, as Hermes refuses a write.** Rejected: the Session that learned it may not make room, and the correction is lost.
- **An onboarding flow for the migration.** Rejected: the sort only adds labels, so there is nothing for the user to decide; the card and the Settings line are enough. Reviewing or editing single entries in Settings is a feature of its own.

## Consequences

- On the user's data (gemini-3.8-flash), the sort filed 85 of 134 entries under 17 topics in under 30 seconds; the core went from 39k to 11k characters, and all 13 of its corrections reach a Session. A consolidation after it took a minute and a half and left 128 entries.

- Global Memory's size limits (40k soft, 60k hard) count the core and the topics together.
- `global.md` lists the core by category, then a section per topic.
- The corrections the hook injects are the only ones left out of "never found"; a topic's corrections count, as a search has to find them.
- Moving an entry to a Project Memory drops its topic.
