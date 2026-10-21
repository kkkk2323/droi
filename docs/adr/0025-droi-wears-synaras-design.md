---
status: accepted
---

# Droi wears Synara's design

Droi's look came from the web Client's `global.css`: Tailwind's greys on `oklch`, Geist Sans and
Geist Mono, a 6–10px radius and full-height rows. It read well but dated: surfaces stepped grey by
grey, borders as solid colours that fought the fills beside them, and code and interface set in the
same family. The web Client is gone (it was removed after 1.59.0), so nothing anchors the look to
that stylesheet any more, and the native app and the Phone App were carrying a copy of it each.

[Synara](https://github.com/Emanuele-web04/synara) separates navigation from work: a grey rail beside
a white reading surface, compact system typography, restrained separators and an inset composer.
Droi follows that composition as well as its material. The Light rail is `#ebebeb` beside `#ffffff`;
Dark uses `#202124` beside `#18191b`. Blue remains reserved for focus and control state, and code
uses JetBrains Mono. Solarized Light+ keeps its cream and parchment palette.

We decided:

- **The tokens are Droi's, shaped after Synara's.** `pkg/theme` keeps its names (`Background`,
  `Card`, `Border`, `Sidebar`, …) so no view had to learn a new vocabulary, and gains `Tint` for the
  one accent a switch or a focused field paints with. Light, Dark and Solarized Light+ are all set in
  the same material: a page, panels a hairline apart, low-alpha fills. Solarized Light+ keeps its
  palette (cream, parchment, base01 text) but softens into it, and takes Solarized's blue as its tint.
- **The interface face is the system's; the code face is vendored.** `SystemUI` (`system-ui`: SF Pro on
  macOS, Segoe UI on Windows) is the default, as Synara does, and JetBrains Mono ships in
  `apps/native/assets/fonts` for code and for fields that are mono. The Font setting still offers the
  vendored Geist pair, and both faces stay registered, so choosing Geist changes nothing else.
- **Geometry follows the workbench composition.** The native sidebar is 224px with compact
  Workspace headings, inset Session rows, a brand header and a labelled Settings action. Session
  chrome and reading text use a 12–13px scale; New session groups its question and composer in
  the centre and scrolls when a long draft or attachments outgrow a short window. Both composers
  use an 18px radius
  and two visible input lines. Settings uses flat groups with hairline separators, 28px navigation
  rows and a centred content column. Shared controls remain in `pkg/kit`.
- **The Phone App takes the same palettes, not the desktop type scale.**
  `apps/mobile/src/ui/theme.ts` holds the same palettes, loads JetBrains Mono
  through `expo-font`, and sets text in the system face with `fontWeight`.
  Session titles and transcript stay at 15px (`fontSize.base`), next to iOS's
  17pt body text: Synara's 13px scale is a desktop scale. The
  `@expo-google-fonts/geist*` packages are gone.
- **Behaviour does not change.** Every control, route, setting, handler and localized label remains
  available. Layout, typography, colour, spacing and touch targets are presentation changes only.

## Considered Options

- **Ship a theme beside the three.** Rejected: the point was the material, not a fourth palette; a
  fourth theme doubles what every screenshot and every test has to cover, and the old look is not
  worth keeping.
- **Copy Synara's code.** Rejected: it is a web app with React and Tailwind; the tokens and the
  component metrics are the parts that carry the look, and those are a table of numbers.
- **Keep the interface in Geist and change only the colours.** Rejected: the type is most of the
  difference. Geist's tight, geometric text is what made the old window read as a web page.
- **Follow Synara's tokens literally, including its oklch values.** Rejected: MyGo draws into
  float16 drawables on a Display P3 screen for colours beyond sRGB, which costs about 50 MB of
  window memory. Hex steps of the same palette stay in sRGB and look the same.

## Consequences

- The view tests' reference screens (the removed web Client's) no longer describe the app: their
  pixel differences measure this departure rather than port fidelity. The screenshots on
  [PR #164](https://github.com/kkkk2323/droi/pull/164) show the current design in Light, Dark and Solarized Light+. They are review
  evidence, not replacement test baselines. `apps/native/README.md` documents how to render them.
- `Tint` is the only accent in the tokens beyond the status colours: a view that needs attention
  paints with it, and nothing else should grow a colour of its own.
- A new face is a download: JetBrains Mono adds about 300 KB to the native
  app (the variable TTF) and two static TTFs of about 270 KB each to the Phone
  App.
- The Phone App's `fonts` object is now two faces and two weights (regular and medium); a weight
  beyond those needs its own file added to `apps/mobile/assets/fonts` and registered in
  `_layout.tsx`.
