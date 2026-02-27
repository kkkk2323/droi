# User Testing

Testing surface: tools, URLs, setup steps, isolation notes, known quirks.

---

## Testing Surface
- **Application type:** Electron desktop app with Web UI mode
- **Start command (Electron):** `pnpm dev` (launches Electron in dev mode)
- **Start command (Web UI):** `DROID_WEB_ENABLED=true DROID_APP_API_HOST=127.0.0.1 DROID_APP_API_PORT=3099 node --experimental-strip-types src/server/index.ts`
- **Web UI URL:** http://127.0.0.1:3099
- **Validation command:** `pnpm check` (format + lint + typecheck)
- **Build command:** `pnpm build` (must run before starting web server)

## Testing Approach
The app can be tested via browser automation using the Web UI mode (standalone server serving the built renderer).
The Web UI renders the same React components as the Electron app, so structural/layout assertions can be verified.

**Limitations:**
- No real Droid CLI sessions available without `FACTORY_API_KEY` — the app loads but shows empty state
- Session-specific assertions (running indicators, pending requests, beep sounds) require mock data or code-level verification
- Chat auto-scroll assertions require an active chat session with messages

**What CAN be verified via browser:**
- Sidebar layout structure (New Project button position, ScrollArea presence, Settings footer position)
- Component rendering and CSS classes
- DOM structure confirming correct component hierarchy

**What requires code-level verification:**
- Chat auto-scroll logic (useEffect watching hasFooterContent, scrollBy behavior)
- Session status indicator logic (needsAttention → AlertCircle vs Loader2)
- Beep behavior (useAttentionBeep hook, initializedRef preventing initial beep)

## Known Quirks
- App requires `FACTORY_API_KEY` to connect to Droid CLI
- Web server must serve built output from `out/renderer/` — run `pnpm build` first
- The standalone server runs on port 3099 (chosen to avoid conflicts)
- Web UI mode uses `isBrowserMode()` which changes some behavior (e.g., hides "New Project" button)

## Flow Validator Guidance: Browser (Web UI)

**Surface:** Web UI at http://127.0.0.1:3099
**Tool:** agent-browser skill
**Isolation:** Single browser session sufficient — read-only verification, no data mutation

Assertions testable via browser:
- VAL-SIDEBAR-002: Verify ScrollArea component wraps the project list (check DOM structure)
- VAL-SIDEBAR-003: Verify Settings footer is in SidebarFooter (check DOM structure)
- VAL-CROSS-001: Verify app renders without errors, sidebar and main content coexist

Note: VAL-SIDEBAR-001 (New Project button) is hidden in browser mode (`isBrowserMode()` returns true), so it must be verified via code inspection.

**Boundaries:**
- Do NOT create sessions or modify any data
- Do NOT attempt to interact with Droid CLI features
- Focus on DOM structure and layout verification only

## Flow Validator Guidance: Code Inspection

**Surface:** Source code files
**Tool:** Read, Grep tools (no browser needed)

Assertions testable via code inspection:
- VAL-SIDEBAR-001: Verify New Project button is rendered outside SidebarContent, above ScrollArea
- VAL-CHAT-001, VAL-CHAT-002, VAL-CHAT-003: Verify auto-scroll useEffect logic in ChatView.tsx
- VAL-STATUS-001, VAL-STATUS-002, VAL-STATUS-003: Verify conditional icon rendering in SessionItem
- VAL-STATUS-004, VAL-STATUS-005: Verify useAttentionBeep hook logic
- VAL-CROSS-001: Verify pnpm check passes (already confirmed)
