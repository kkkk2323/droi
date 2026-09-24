// The web Client's activity marks, not iOS's own spinner: a turning Loader2
// (Tailwind's animate-spin), three bouncing dots for what the Daemon is doing
// (animate-bounce), and a pulsing dot (animate-pulse).
import { LoaderCircle } from 'lucide-react-native'
import { useEffect, useState } from 'react'
import { Animated, Easing, StyleSheet, View } from 'react-native'

/** Runs the animation forever; the returned cleanup stops it. For an effect. */
function loop(animation: Animated.CompositeAnimation): () => void {
  const running = Animated.loop(animation)
  running.start()
  return () => running.stop()
}

// Every Spinner turns on this one value, so ones that appear at different
// moments (a column of tool rows) point the same way. It runs while any shows.
const turn = new Animated.Value(0)
const rotate = turn.interpolate({ inputRange: [0, 1], outputRange: ['0deg', '360deg'] })
let spinners = 0
let stopTurning: (() => void) | null = null

function useSharedTurn() {
  useEffect(() => {
    spinners += 1
    stopTurning ??= loop(
      Animated.timing(turn, {
        toValue: 1,
        duration: 1000,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    )
    return () => {
      spinners -= 1
      if (spinners > 0) return
      stopTurning?.()
      stopTurning = null
    }
  }, [])
}

export function Spinner({ size, color }: { size: number; color: string }) {
  useSharedTurn()
  return (
    <Animated.View aria-hidden style={{ width: size, height: size, transform: [{ rotate }] }}>
      <LoaderCircle size={size} color={color} strokeWidth={2} />
    </Animated.View>
  )
}

const DOT = 4

function BouncingDot({ color, delay }: { color: string; delay: number }) {
  // Up a quarter of its size and back, once a second, as animate-bounce.
  const [lift] = useState(() => new Animated.Value(0))
  useEffect(
    () =>
      loop(
        Animated.sequence([
          Animated.delay(delay),
          Animated.timing(lift, {
            toValue: 1,
            duration: 500,
            easing: Easing.bezier(0, 0, 0.2, 1),
            useNativeDriver: true,
          }),
          Animated.timing(lift, {
            toValue: 0,
            duration: 500 - delay,
            easing: Easing.bezier(0.8, 0, 1, 1),
            useNativeDriver: true,
          }),
        ]),
      ),
    [lift, delay],
  )
  const translateY = lift.interpolate({ inputRange: [0, 1], outputRange: [-DOT / 4, 0] })
  return (
    <Animated.View style={[styles.dot, { backgroundColor: color, transform: [{ translateY }] }]} />
  )
}

export function BouncingDots({ color }: { color: string }) {
  return (
    <View aria-hidden style={styles.dots}>
      <BouncingDot color={color} delay={0} />
      <BouncingDot color={color} delay={150} />
      <BouncingDot color={color} delay={300} />
    </View>
  )
}

export function PulsingDot({ color, size = 6 }: { color: string; size?: number }) {
  const [fade] = useState(() => new Animated.Value(0))
  useEffect(
    () =>
      loop(
        Animated.sequence([
          Animated.timing(fade, { toValue: 1, duration: 1000, useNativeDriver: true }),
          Animated.timing(fade, { toValue: 0, duration: 1000, useNativeDriver: true }),
        ]),
      ),
    [fade],
  )
  const opacity = fade.interpolate({ inputRange: [0, 1], outputRange: [1, 0.5] })
  return (
    <Animated.View
      aria-hidden
      style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: color, opacity }}
    />
  )
}

const styles = StyleSheet.create({
  dots: { flexDirection: 'row', alignItems: 'center', gap: 2, height: DOT * 2 },
  dot: { width: DOT, height: DOT, borderRadius: DOT / 2 },
})
