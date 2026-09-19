import { useEffect, useState } from 'react'
import { useSettingsFlashAt } from '@/store'

export function SettingsFlashIndicator({ className }: { className?: string }) {
  const flashAt = useSettingsFlashAt()
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (!flashAt) return
    setVisible(true)
    const timer = setTimeout(() => setVisible(false), 2000)
    return () => clearTimeout(timer)
  }, [flashAt])

  if (!visible) return null

  return (
    <span
      className={`text-[10px] text-amber-500 transition-opacity duration-500 ${className || ''}`}
    >
      Settings synced
    </span>
  )
}
