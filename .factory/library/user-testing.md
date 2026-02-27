# User Testing

Testing surface: tools, URLs, setup steps, isolation notes, known quirks.

---

## Testing Surface
- **Application type:** Electron desktop app
- **Start command:** `pnpm dev` (launches Electron in dev mode)
- **Validation command:** `pnpm check` (format + lint + typecheck)

## Manual Testing
- Sidebar scroll: Need enough projects/sessions to overflow the sidebar
- Chat scroll: Need an active session, send a message and observe Generating indicator
- Session indicators: Need a session with a pending permission request (trigger by running a tool that requires confirmation)

## Known Quirks
- App requires `FACTORY_API_KEY` to connect to Droid CLI
- Electron app — cannot be tested via browser automation tools
- Visual verification is primary testing method for these UI fixes
