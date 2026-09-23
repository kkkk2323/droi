import { cn } from '@/lib/utils'

/**
 * Droi's own mark, the "D" with a prompt from the app icon
 * (resources/icon.svg), without the dark tile and glow so it sits on the
 * page. The greens are the icon's; the darker one reads on the light theme.
 */
export function DroiMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="120 90 340 340"
      fill="none"
      aria-hidden
      className={cn('text-[#2b9e5e] dark:text-[#3DDC84]', className)}
    >
      <path
        d="M160 100H260C348.366 100 420 171.634 420 260C420 348.366 348.366 420 260 420H160V100Z"
        stroke="currentColor"
        strokeWidth="20"
      />
      <path
        d="M220 220L260 260L220 300"
        stroke="currentColor"
        strokeWidth="20"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <rect x="280" y="285" width="50" height="15" rx="4" fill="currentColor" />
    </svg>
  )
}
