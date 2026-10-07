const fs = require('fs')
let src = fs.readFileSync(process.argv[2], 'utf8')
src = src
  .replace(/^import .*$/m, '')
  .replace('export const solarizedLightPlus: ThemeInput =', 'module.exports =')
const m = { exports: {} }
new Function('module', src)(m)
fs.writeFileSync(process.argv[3], JSON.stringify(m.exports, null, 1) + '\n')
