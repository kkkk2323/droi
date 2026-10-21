## Design Context

### Users
Professional developers who use Droi as their desktop AI coding assistant. They are typically in a focused, deep-work state and need an interface that stays out of the way while providing powerful capabilities. The tool is used during active development sessions where efficiency and low friction are paramount.

### Brand Personality
**Professional, Restrained, Efficient.**
The interface should feel like a precision instrument -- quiet confidence rather than flashy decoration. Every element earns its place. Users should feel empowered and in control, never overwhelmed or distracted.

### Aesthetic Direction
- **Visual tone**: Refined and information-dense, inspired by Linear and Raycast, and by Synara's material (ADR 0025): a near-white page with panels a hairline apart, faint alpha fills rather than extra greys, tight controls.
- **References**: Linear (polish, dark-mode-first, subtle animations), Raycast (command-driven efficiency, minimal chrome), Synara (surfaces, hairlines, the single tint).
- **Anti-references**: Overly playful or colorful interfaces (Slack-style), heavy illustrations or mascots, excessive whitespace that wastes screen real estate.
- **Theme**: Light mode is the primary experience (a near-white page like Synara's). Dark mode is a first-class toggle, stored per browser. Solarized Light+ is kept, drawn in the same material.
- **Typography**: the system's interface font (San Francisco, Segoe UI) for UI, JetBrains Mono for code. Geist stays as Settings → Font's choice. No decorative fonts.
- **Color**: neutral palette with one blue tint (`#0169cc` light, `#339cff` dark) for the controls that show state. Color is reserved for status, actions, and that tint -- never for decoration.
- **Geometry**: 28–32px controls, 8–12px radii, 13px base text with 20px/500 headings and −0.3px tracking.
- **Motion**: Subtle and purposeful (framer-motion). Animations should feel responsive, not theatrical. Prefer fast durations (150-250ms) with cubic-bezier easing.

### Design Principles

1. **Information density over whitespace** -- Developers value seeing more context at once. Optimize for information density while maintaining readability. Every pixel should serve a purpose.

2. **Quiet until needed** -- UI chrome, controls, and decorative elements should recede into the background. Surface them contextually when relevant. Scrollbars appear on hover, actions appear on interaction.

3. **Speed is a feature** -- Interactions should feel instant. Prefer skeleton states over spinners, optimistic updates over loading screens. Animations are functional (guiding attention), not decorative.

4. **Consistency through constraint** -- Use the established component library (shadcn/ui + Base UI Vega). Resist adding one-off styles. Stick to the design token system (CSS variables, Tailwind utilities). New patterns must justify their existence.

5. **Code is the content** -- The chat and code output are the primary focus. All surrounding UI (sidebar, input bar, status indicators) exists to support the content area, not compete with it.

6. **Accessible by construction** -- Every interactive element has a real role and an accessible name; state is exposed through `aria-current`, `aria-expanded`, `aria-live` and friends, not colour alone. Everything works from the keyboard and honours `prefers-reduced-motion`. The E2E suite selects by role and label, so an element a screen reader cannot name is an element the tests cannot find.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues on `kkkk2323/droi` (via `gh`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-label vocabulary (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` at the root and ADRs in `docs/adr/`. See `docs/agents/domain.md`.
