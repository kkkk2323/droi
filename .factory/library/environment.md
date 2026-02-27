# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** Required env vars, external API keys/services, dependency quirks, platform-specific notes.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

- `FACTORY_API_KEY` — Required for Droid CLI authentication
- Node.js + pnpm required
- Electron app — runs as desktop application via `pnpm dev`
