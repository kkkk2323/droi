import { useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

interface AnimatedNumberProps {
  value: number
  className?: string
}

export function AnimatedNumber({ value, className }: AnimatedNumberProps) {
  const [displayValue, setDisplayValue] = useState(value)
  const [animating, setAnimating] = useState<'up' | 'down' | null>(null)
  const prevRef = useRef(value)

  useEffect(() => {
    if (prevRef.current === value) return
    const dir = value > prevRef.current ? 'up' : 'down'
    prevRef.current = value
    setAnimating(dir)
    const t = setTimeout(() => {
      setDisplayValue(value)
      setAnimating(null)
    }, 180)
    return () => clearTimeout(t)
  }, [value])

  return (
    <span className={cn('inline-flex overflow-hidden', className)}>
      <span
        className={cn(
          'inline-block tabular-nums transition-transform duration-[180ms] ease-out',
          animating === 'up' && '-translate-y-full',
          animating === 'down' && 'translate-y-full',
        )}
      >
        {displayValue}
      </span>
    </span>
  )
}
