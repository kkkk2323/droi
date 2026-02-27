# Architecture

Architectural decisions, patterns discovered during the mission.

---

## Sidebar Layout
- `Sidebar` component wraps: `SidebarHeader` (fixed top) + `SidebarContent` (scrollable middle) + `SidebarFooter` (fixed bottom)
- `SidebarContent` has `overflow-auto` and `flex-1` — it's the natural scroll container
- `ScrollArea` from `components/ui/scroll-area.tsx` wraps `@base-ui/react/scroll-area` with custom styled scrollbar

## Chat Scroll
- Chat uses `react-virtuoso` (`Virtuoso` component) with `followOutput` for auto-scroll
- Footer content (Generating indicator, SpecReviewCard) rendered via Virtuoso's `components.Footer` prop
- `atBottomThreshold` is 40px — determines when user is considered "at bottom"
- `followOutput` returns `'smooth'` when at bottom, `false` otherwise

## Session State
- Per-session state stored in `sessionBuffers` Map<string, SessionBuffer> in Zustand store
- `SessionBuffer.pendingPermissionRequests` — array of pending permission requests
- `SessionBuffer.pendingAskUserRequests` — array of pending AskUser requests
- `SessionBuffer.isRunning` — boolean, true when session is actively processing
- `SessionBuffer.workingState` — string describing current working state
