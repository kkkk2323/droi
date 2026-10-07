// Copies the SDK bundle and exports every top-level zod schema, so a
// generator can walk them.
const fs = require('fs')
const [src, out] = process.argv.slice(2)
let text = fs.readFileSync(src, 'utf8')
const names = new Set()
for (const m of text.matchAll(/^var (\w+Schema\w*) = /gm)) names.add(m[1])
// Also enums used as nativeEnum / value lists.
const body = text.replace(/\nexport \{[\s\S]*?\};\s*$/, '\n')
const enums = new Set()
for (const m of text.matchAll(/^var (\w+) = \/\* @__PURE__ \*\/ \(\((\w+)\) => \{/gm))
  enums.add(m[1])
fs.writeFileSync(
  out,
  body +
    '\nexport const __schemas = {\n' +
    [...names].map((n) => `  ${n}`).join(',\n') +
    '\n};\nexport const __enums = {\n' +
    [...enums].map((n) => `  ${n}`).join(',\n') +
    '\n};\n',
)
console.log('enums', enums.size)
console.log('schemas', names.size)
