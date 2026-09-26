import { cn } from '@/lib/utils'

/**
 * Droi's own mark, the geometric "D" with its green dot from the app icon
 * (resources/icon.svg), without the tile so it sits on the page. The D
 * takes the text colour, so it reads on both themes; the dot stays the icon's green.
 */
export function DroiMark({ className }: { className?: string }) {
  return (
    <svg viewBox="302 292 440 440" aria-hidden className={cn('text-foreground', className)}>
      <rect x="346" y="292" width="112" height="440" rx="36" fill="currentColor" />
      <path
        d="M478 328Q478 292 514 294.97A220 220 0 0 1 514 729.03Q478 732 478 696Z"
        fill="currentColor"
      />
      <circle cx="571" cy="512" r="54" fill="#34c77b" />
    </svg>
  )
}
