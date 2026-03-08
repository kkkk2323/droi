## Design Context

### Users
Professional developers who use Droi as their desktop AI coding assistant. They are typically in a focused, deep-work state and need an interface that stays out of the way while providing powerful capabilities. The tool is used during active development sessions where efficiency and low friction are paramount.

### Brand Personality
**Professional, Restrained, Efficient.**
The interface should feel like a precision instrument -- quiet confidence rather than flashy decoration. Every element earns its place. Users should feel empowered and in control, never overwhelmed or distracted.

### Aesthetic Direction
- **Visual tone**: Refined and information-dense, inspired by Linear and Raycast. Clean lines, tight spacing, high signal-to-noise ratio.
- **References**: Linear (polish, dark-mode-first, subtle animations), Raycast (command-driven efficiency, minimal chrome).
- **Anti-references**: Overly playful or colorful interfaces (Slack-style), heavy illustrations or mascots, excessive whitespace that wastes screen real estate.
- **Theme**: Dark mode is the primary experience. Light mode supported but secondary.
- **Typography**: Geist Sans for UI, Geist Mono for code. No decorative fonts.
- **Color**: Neutral palette (oklch-based) with minimal accent usage. Color is reserved for status, actions, and differentiation -- never for decoration.
- **Motion**: Subtle and purposeful (framer-motion). Animations should feel responsive, not theatrical. Prefer fast durations (150-250ms) with cubic-bezier easing.

### Design Principles

1. **Information density over whitespace** -- Developers value seeing more context at once. Optimize for information density while maintaining readability. Every pixel should serve a purpose.

2. **Quiet until needed** -- UI chrome, controls, and decorative elements should recede into the background. Surface them contextually when relevant. Scrollbars appear on hover, actions appear on interaction.

3. **Speed is a feature** -- Interactions should feel instant. Prefer skeleton states over spinners, optimistic updates over loading screens. Animations are functional (guiding attention), not decorative.

4. **Consistency through constraint** -- Use the established component library (shadcn/ui + Base UI Vega). Resist adding one-off styles. Stick to the design token system (CSS variables, Tailwind utilities). New patterns must justify their existence.

5. **Code is the content** -- The chat and code output are the primary focus. All surrounding UI (sidebar, input bar, status indicators) exists to support the content area, not compete with it.
