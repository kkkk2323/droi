// The text size preference scales the transcript and the composer, not the
// app's chrome. Text inside a <TextScale> grows or shrinks with it.
import { usePreference } from '@droi/daemon-layer/local-preference'
import { TEXT_SCALE, textSize } from '@droi/daemon-layer/text-size'
import { createContext, use, type ReactNode } from 'react'

const TextScaleContext = createContext(1)

export function TextScale({ children }: { children: ReactNode }) {
  const [size] = usePreference(textSize)
  return <TextScaleContext value={TEXT_SCALE[size]}>{children}</TextScaleContext>
}

export function useTextScale(): number {
  return use(TextScaleContext)
}
