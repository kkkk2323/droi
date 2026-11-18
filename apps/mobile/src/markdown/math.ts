// TeX typeset as SVG for the Phone App's Markdown: React Native has no
// layout engine for math, so MathJax lays the formula out in JavaScript and
// react-native-svg draws the paths it hands back.
import { liteAdaptor } from 'mathjax-full/js/adaptors/liteAdaptor.js'
import { RegisterHTMLHandler } from 'mathjax-full/js/handlers/html.js'
import { TeX } from 'mathjax-full/js/input/tex.js'
import 'mathjax-full/js/input/tex/ams/AmsConfiguration.js'
import 'mathjax-full/js/input/tex/base/BaseConfiguration.js'
import 'mathjax-full/js/input/tex/boldsymbol/BoldsymbolConfiguration.js'
import 'mathjax-full/js/input/tex/newcommand/NewcommandConfiguration.js'
import { mathjax } from 'mathjax-full/js/mathjax.js'
import { SVG } from 'mathjax-full/js/output/svg.js'

export interface Formula {
  /** An `<svg>` with a viewBox and no size of its own; the caller sizes it. */
  svg: string
  /** Sizes in em of the surrounding text. */
  width: number
  height: number
  /** How far the formula reaches below the text's baseline. */
  depth: number
}

// MathJax sizes its SVG in ex of its own TeX font.
const EX = 0.442

let document: ReturnType<typeof createDocument> | null = null

function createDocument() {
  const adaptor = liteAdaptor()
  RegisterHTMLHandler(adaptor)
  const doc = mathjax.document('', {
    InputJax: new TeX({
      packages: ['base', 'ams', 'newcommand', 'boldsymbol'],
      // TeX MathJax cannot read is shown as its source, not as a red error box.
      formatError: (_jax: unknown, error: Error) => {
        throw error
      },
    }),
    // Paths inline: react-native-svg's <use> does not reach across a document.
    OutputJax: new SVG({ fontCache: 'none' }),
  })
  return { adaptor, doc }
}

const CACHE_SIZE = 200
const cache = new Map<string, Formula | null>()

/** The formula typeset, or null when the TeX does not parse. */
export function typeset(tex: string, display: boolean): Formula | null {
  const key = (display ? 'D' : 'I') + tex
  if (cache.has(key)) return cache.get(key)!
  const formula = convert(tex, display)
  if (cache.size >= CACHE_SIZE) cache.delete(cache.keys().next().value!)
  cache.set(key, formula)
  return formula
}

function convert(tex: string, display: boolean): Formula | null {
  document ??= createDocument()
  const { adaptor, doc } = document
  let svg
  try {
    svg = adaptor.firstChild(doc.convert(tex, { display })) as ReturnType<typeof adaptor.node>
  } catch {
    return null
  }
  const ex = (name: string) => parseFloat(String(adaptor.getAttribute(svg, name) ?? '')) || 0
  const width = ex('width') * EX
  const height = ex('height') * EX
  const align = /vertical-align:\s*(-?[\d.]+)ex/.exec(String(adaptor.getAttribute(svg, 'style')))
  const depth = align ? -parseFloat(align[1]!) * EX : 0
  for (const name of ['width', 'height', 'style']) adaptor.removeAttribute(svg, name)
  return { svg: adaptor.outerHTML(svg), width, height, depth }
}
