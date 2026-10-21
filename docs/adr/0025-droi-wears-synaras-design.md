---
status: accepted
---

# Droi wears Synara's design

Droi's look came from the web Client's `global.css`: Tailwind's greys on `oklch`, Geist Sans and
Geist Mono, a 6–10px radius and full-height rows. It read well but dated: surfaces stepped grey by
grey, borders as solid colours that fought the fills beside them, and code and interface set in the
same family. The web Client is gone (it was removed after 1.59.0), so nothing anchors the look to
that stylesheet any more, and the native app and the Phone App were carrying a copy of it each.

[Synara](https://github.com/Emanuele-web04/synara) does the same job with less: a near-white page
(`#fcfcfc`) with white panels, a near-black one (`#0e0e0e`) with `#131313` panels, borders and fills
as single-digit alpha steps of the ink rather than greys of their own, one blue tint for the controls
that show state, the system's interface face, and JetBrains Mono for code. We borrowed that material,
not Synara's screens or its features.

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
- **Geometry follows the reference, not the old stylesheet.** Buttons 28–32px high at 13px with an 8px
  radius, fields 32px, switches 32×20, setting cards 12px with 10/12px rows, sidebar rows 28px,
  user bubbles 14px of radius, headings 20px at weight 500 with −0.3px of tracking. Sizes come from
  `pkg/kit`, so a view that already used a button or a setting row moved with it.
- **The Phone App takes the same tokens.** `apps/mobile/src/ui/theme.ts` holds the same palettes and
  type scale, loads JetBrains Mono through `expo-font`, and sets text in the system face with
  `fontWeight`. The `@expo-google-fonts/geist*` packages are gone.
- **Behaviour does not change.** No view gained or lost a control, no route, setting or label moved,
  and the l10n catalogues changed only where a description named the old font.

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
  pixel difference is now the measure of this departure (2–8% per screen) rather than of port
  fidelity, and they cannot be re-recorded, since the Client that recorded them is gone.
  `apps/native/README.md` says so; re-recording from the native app itself would turn the comparison
  into a self-portrait and hide the drift.
- `Tint` is the only accent in the tokens beyond the status colours: a view that needs attention
  paints with it, and nothing else should grow a colour of its own.
- A new face is a download: JetBrains Mono adds about 200 KB to the app (the variable TTF, one file).
- The Phone App's `fonts` object is now two faces and two weights (regular and medium); a weight
  beyond those needs its own file added to `apps/mobile/assets/fonts` and registered in
  `_layout.tsx`.
