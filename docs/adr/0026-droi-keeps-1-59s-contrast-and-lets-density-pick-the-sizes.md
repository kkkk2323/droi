---
status: accepted
---

# Droi keeps 1.59's contrast and lets Density pick the sizes

ADR 0025 gave Droi Synara's composition and material in 1.60. In use, the window read as washed out.
Text went from `#161616` to `#262626` and muted text from `#636363` to `#6f6f6f`. Hairlines went
from a solid `#dedede` to 6% ink (about `#ededed`, a contrast of 1.14 on white). The composer, the
todo bar and the menus are white panels on a white page, so with those hairlines they no longer
stood apart from what was around them. Inline code lost its colour, and the text and controls were
1–2px smaller than in 1.59.

We decided:

- **Contrast comes back from 1.59; the composition stays from 1.60.** Light text is `#171717`, muted
  text `#636363`, hairlines 12% ink and inputs 15%, close to 1.59's `#dedede` and `#d7d7d7`. Dark
  hairlines are 12% white and inputs 15%. Inline code takes a colour again (`#af2938` in Light,
  `#fb9797` in Dark, `#1f7f78` in Solarized Light+; the Phone App keeps its warm ink). The grey rail,
  the white reading surface, the single tint and JetBrains Mono stay. The Phone App takes the same
  colours.
- **Settings → Density picks the sizes, Comfortable by default.** Comfortable keeps 1.59's sizes
  where 1.60 made them smaller: 14px base text, a 15px transcript, 32px rows and controls, 36px
  fields. Compact keeps 1.60's: 13px, 28px, 30px. A view picks its size with `kit.Dense(compact,
  comfortable)`, and a Markdown style with `Roomy`. Text size still scales either one, and the Phone
  App, which kept its 15px scale, has no density.

## Considered Options

- **A switch between the 1.59 and the 1.60 look.** Rejected, for ADR 0025's reason against a fourth
  theme: two looks double every screenshot and test, and most of what differs is not a preference.
- **Contrast as a setting.** Rejected: text that is hard to read is a fault, not a taste.

## Consequences

- A size that changes with the density is written as `k.Dense(compact, comfortable)`. A size that
  1.60 did not change stays a plain number.
- WeCom Droi builds its own kit with `kit.New` and gets Comfortable unless it sets `Kit.Compact`.
