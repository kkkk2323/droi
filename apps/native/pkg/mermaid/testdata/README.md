# Test diagrams

`corpus/` holds diagrams copied unchanged from Ondine's test vectors
(`test/vectors` at commit `8d42a0aa603d14e488c46dc91723f87d0f0d4a44`), which
Ondine in turn copied from Pele at `458322a5b8abb1ae57ddf127c32017ab9fb14a92`:

- `docs-flowchart-NNN.mmd`, `docs-sequence-NNN.mmd`: entry NNN of
  `corpus/flowchart.json` and `corpus/sequence.json`, examples from Mermaid's
  syntax documentation ([`LICENSE-mermaid`](LICENSE-mermaid), MIT).
- `layout-<group>-NN.mmd`: case NN of group `<group>` in `layout-cases.json`,
  Pele's layout cases ([`LICENSE-pele`](LICENSE-pele), MIT).

Every file must render; the tests also check the layout of each flowchart.
