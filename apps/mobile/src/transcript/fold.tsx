// A fold in a transcript row (tool details, reasoning, a subagent's report):
// what opens or closes tells the list by how much the row grew, in the same
// React commit as the change, so the list can scroll by as much in the same
// native update and the header the reader tapped stays under their finger.
// A row's height is only known once laid out, so opening first lays the
// content out invisibly, out of the flow, and puts it in the flow once measured.
import { createContext, useContext, useRef, useState, type ReactNode } from 'react'
import { StyleSheet, View, type LayoutChangeEvent } from 'react-native'

/** Told by how many points a row grew (negative: shrank) from a fold in it opening or closing. */
export const OnGrow = createContext<(by: number) => void>(() => {})

export interface Fold {
  open: boolean
  toggle: () => void
  /** For Folded. */
  measuring: boolean
  onLayout: (event: LayoutChangeEvent) => void
  onMeasured: (event: LayoutChangeEvent) => void
}

export function useFold(initiallyOpen = false): Fold {
  const grow = useContext(OnGrow)
  const [open, setOpen] = useState(initiallyOpen)
  const [measuring, setMeasuring] = useState(false)
  const height = useRef(0)
  return {
    open,
    measuring,
    toggle: () => {
      if (measuring) return
      if (open) {
        grow(-height.current)
        setOpen(false)
      } else {
        setMeasuring(true)
      }
    },
    onLayout: (event) => {
      height.current = event.nativeEvent.layout.height
    },
    onMeasured: (event) => {
      height.current = event.nativeEvent.layout.height
      grow(height.current)
      setMeasuring(false)
      setOpen(true)
    },
  }
}

/** The fold's content: absent while closed, laid out invisibly while measuring, then in the flow. */
export function Folded({ fold, children }: { fold: Fold; children: ReactNode }) {
  if (!fold.open && !fold.measuring) return null
  // The outer View gives the measured content the row's width without a
  // parent's padding; the inner one keeps its identity from measuring to open.
  return (
    <View>
      <View
        aria-hidden={fold.measuring}
        style={fold.measuring ? styles.measuring : null}
        onLayout={fold.measuring ? fold.onMeasured : fold.onLayout}
      >
        {children}
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  measuring: { position: 'absolute', left: 0, right: 0, opacity: 0, pointerEvents: 'none' },
})
