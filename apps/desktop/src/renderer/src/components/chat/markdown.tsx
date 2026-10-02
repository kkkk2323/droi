import type { ComponentProps } from 'react'
import { code, type ThemeInput } from '@streamdown/code'
import { createMathPlugin } from '@streamdown/math'
import { Streamdown, type Components } from 'streamdown'
import { localImagePath, useLocalImage } from '@droi/daemon-layer/local-image'
import { solarizedLightPlus } from '@/lib/solarized-light-plus'
import { useTheme, type Theme } from '@/lib/theme'
import { cn } from '@/lib/utils'

/** Assistant prose. Streamdown tolerates the unterminated markdown of a live stream. */
export function Markdown({ text, className }: { text: string; className?: string }) {
  const [theme] = useTheme()
  return (
    <Streamdown
      className={cn('prose-droi max-w-none text-[15px] leading-7', className)}
      components={COMPONENTS}
      plugins={PLUGINS}
      shikiTheme={CODE_THEMES[theme]}
    >
      {text}
    </Streamdown>
  )
}

const COMPONENTS: Components = { img: MarkdownImage }

// Droid is told to write inline math between single dollars.
const PLUGINS = { code, math: createMathPlugin({ singleDollarTextMath: true }) }

// Streamdown paints code in the first theme and in the second under `.dark`.
const CODE_THEMES: Record<Theme, [ThemeInput, ThemeInput]> = {
  light: ['github-light', 'github-dark'],
  dark: ['github-light', 'github-dark'],
  'solarized-light': [solarizedLightPlus, 'github-dark'],
}

const IMAGE = 'my-4 max-w-full rounded-lg'

/** An image the agent wrote a path to comes from the computer through the Daemon. */
function MarkdownImage({
  src,
  alt,
  node: _node,
  ...rest
}: ComponentProps<'img'> & { node?: unknown }) {
  const path = typeof src === 'string' ? localImagePath(src) : null
  if (path === null) return <img src={src} alt={alt ?? ''} className={IMAGE} {...rest} />
  return <LocalImage path={path} alt={alt ?? ''} />
}

function LocalImage({ path, alt }: { path: string; alt: string }) {
  const image = useLocalImage(path)
  if (image.status === 'ready') {
    return <img src={image.src} alt={alt} title={path} className={IMAGE} />
  }
  if (image.status === 'loading') {
    return (
      <span
        role="img"
        aria-label={alt || 'Loading image'}
        aria-busy
        className={cn(IMAGE, 'block h-32 w-64 animate-pulse bg-muted')}
      />
    )
  }
  return (
    <span className="my-4 block text-xs text-muted-foreground italic">
      Image not available: <code className="not-italic">{path}</code>
    </span>
  )
}
