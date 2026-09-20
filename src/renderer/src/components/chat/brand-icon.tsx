import { Sparkles } from 'lucide-react'
import anthropic from '@/assets/providers/anthropic.svg'
import deepseek from '@/assets/providers/deepseek.svg'
import google from '@/assets/providers/google.svg'
import minimax from '@/assets/providers/minimax.svg'
import moonshot from '@/assets/providers/moonshot.svg'
import zhipu from '@/assets/providers/zhipu.svg'
import { cn } from '@/lib/utils'
import type { Brand } from '@/lib/model-brand'

// Colour marks are static SVGs rendered from @lobehub/icons (MIT). OpenAI and
// xAI are single-colour and drawn inline so they follow the text colour.
const COLOR_MARKS: Partial<Record<Brand, string>> = {
  anthropic,
  google,
  zhipu,
  moonshot,
  deepseek,
  minimax,
}

export function BrandIcon({ brand, className }: { brand: Brand; className?: string }) {
  const src = COLOR_MARKS[brand]
  if (src) {
    return <img src={src} alt="" aria-hidden className={cn('size-4 shrink-0', className)} />
  }
  if (brand === 'openai') {
    return (
      <svg
        viewBox="0 0 24 24"
        aria-hidden
        className={cn('size-4 shrink-0 fill-current', className)}
      >
        <path d="M9.205 8.658v-2.26c0-.19.072-.333.238-.428l4.543-2.616c.619-.357 1.356-.523 2.117-.523 2.854 0 4.662 2.212 4.662 4.566 0 .167 0 .357-.024.547l-4.71-2.759a.797.797 0 00-.856 0l-5.97 3.473zm10.609 8.803v-5.399c0-.333-.143-.57-.428-.737L13.415 7.85l1.95-1.118a.484.484 0 01.5 0l4.543 2.617c1.309.76 2.188 2.378 2.188 3.948 0 1.808-1.07 3.472-2.782 4.163zM7.802 12.61l-1.95-1.142a.446.446 0 01-.237-.428V5.804c0-2.545 1.95-4.472 4.59-4.472 1 0 1.928.334 2.712.928L8.23 4.973a.821.821 0 00-.428.737v6.9zM12 15.036l-2.795-1.57v-3.33L12 8.567l2.796 1.57v3.33L12 15.035zm2.212 7.634c-.999 0-1.927-.333-2.712-.928l4.686-2.712a.822.822 0 00.428-.737v-6.9l1.974 1.142a.446.446 0 01.238.428v5.236c0 2.545-1.975 4.471-4.614 4.471zm-6.827-6.85a.484.484 0 01-.5 0L2.34 13.201C1.032 12.443.152 10.825.152 9.255c0-1.832 1.094-3.472 2.807-4.163v5.423c0 .333.142.57.428.737l5.947 3.45-1.95 1.117zm-.238 6.017c-2.688 0-4.71-2.021-4.71-4.567 0-.19.024-.38.048-.57l4.686 2.711a.797.797 0 00.856 0l5.97-3.448v2.26c0 .19-.072.333-.238.428l-4.543 2.616c-.619.357-1.356.523-2.117.523zm.238-1.712a5.962 5.962 0 004.162 1.713 5.947 5.947 0 005.827-4.757c2.665-.69 4.378-3.188 4.378-5.733 0-1.666-.713-3.283-1.998-4.448.119-.5.19-1 .19-1.498 0-3.401-2.759-5.947-5.946-5.947-.642 0-1.26.095-1.88.31A5.962 5.962 0 0010.205 0a5.947 5.947 0 00-5.827 4.757C1.713 5.447 0 7.945 0 10.49c0 1.666.713 3.283 1.998 4.448-.119.5-.19 1-.19 1.499 0 3.401 2.759 5.946 5.946 5.946.642 0 1.26-.095 1.88-.309a5.96 5.96 0 004.162 1.713z" />
      </svg>
    )
  }
  if (brand === 'xai') {
    return (
      <svg
        viewBox="0 0 24 24"
        aria-hidden
        className={cn('size-4 shrink-0 fill-current', className)}
      >
        <path d="M6.469 8.776L16.512 23h-4.464L2.005 8.776H6.47zm-.004 7.9l2.233 3.164L6.467 23H2l4.465-6.324zM22 2.582V23h-3.659V7.764L22 2.582zM22 1l-9.952 14.095-2.233-3.163L17.533 1H22z" />
      </svg>
    )
  }
  return <Sparkles aria-hidden className={cn('size-4 shrink-0', className)} />
}
