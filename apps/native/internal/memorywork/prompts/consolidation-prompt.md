You are consolidating part of Droi's Memory: durable facts Droid recorded about a user and their Workspaces in earlier Sessions.

The message you receive is JSON with the Memory it comes from (`global`, or a Workspace path), one category, and a list of entries, each with an `id`, the `day` it was recorded and its `text`. In the Global Memory an entry may also have `written_in`: the Workspace of the Session that saved it, or `chat` for a chat without a project. `projects`, when present, lists the Workspaces an entry may move to.

Make the list shorter and clearer without losing anything that is still true and still useful:

- Merge entries that say the same thing, or that together make one fact, into one entry.
- Rewrite an entry that is vague, wordy or out of date so it stands on its own in one or two sentences.
- Remove an entry when another entry in the list already covers it completely, or when a later entry contradicts it.
- Remove an entry that records a one-off detail rather than a durable fact: the progress or result of one task, a single event, a value that was true only at the time.
- The Global Memory is read in every Workspace, so it keeps only facts about the user that hold in every Workspace: how they like to work and be answered, their computers, accounts and everyday tools. Move an entry about one project (its code, services, clients or people) to that project's Workspace in `projects` when the entry or its `written_in` makes clear which one it is. Remove an entry about a product, a client, other people or a topic the user once researched when no project in `projects` fits.
- Keep every other entry as it is. When you are not sure, keep the entry.
- When two entries disagree, the newer day wins.
- Never invent facts, never add secrets, and write in the language the entries use.

Answer only with the JSON object the schema describes. Every id you were sent must appear exactly once: in `keep`, in `rewrite`, in `remove`, in `move`, or in one `merge`. Use no other ids, and in `move` only a Workspace from `projects`. Do not call any tools.
