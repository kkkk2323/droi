// The few building blocks every Phone App screen uses, styled from the theme.
import type { ReactNode } from 'react'
import {
  ActivityIndicator,
  Pressable,
  Text as RNText,
  type PressableProps,
  type StyleProp,
  type TextProps,
  type TextStyle,
  type ViewStyle,
} from 'react-native'
import { fontSize, fonts, radius, space } from './theme'
import { useColors } from './use-colors'

type Tone = 'default' | 'muted'

export function Text({
  tone = 'default',
  size = 'base',
  weight = 'regular',
  mono = false,
  style,
  ...props
}: TextProps & {
  tone?: Tone
  size?: keyof typeof fontSize
  weight?: 'regular' | 'medium' | 'semibold'
  mono?: boolean
}) {
  const colors = useColors()
  const family = mono
    ? weight === 'regular'
      ? fonts.mono
      : fonts.monoMedium
    : weight === 'regular'
      ? fonts.sans
      : weight === 'medium'
        ? fonts.sansMedium
        : fonts.sansSemiBold
  return (
    <RNText
      {...props}
      style={[
        {
          color: tone === 'muted' ? colors.mutedForeground : colors.foreground,
          fontFamily: family,
          fontSize: fontSize[size],
        },
        style,
      ]}
    />
  )
}

export function Heading({
  children,
  style,
}: {
  children: ReactNode
  style?: StyleProp<TextStyle>
}) {
  return (
    <Text accessibilityRole="header" size="xl" weight="semibold" style={style}>
      {children}
    </Text>
  )
}

export function Button({
  label,
  variant = 'primary',
  busy = false,
  disabled,
  style,
  ...props
}: Omit<PressableProps, 'style' | 'children'> & {
  label: string
  variant?: 'primary' | 'secondary' | 'ghost'
  busy?: boolean
  style?: StyleProp<ViewStyle>
}) {
  const colors = useColors()
  const inactive = disabled || busy
  const background =
    variant === 'primary' ? colors.primary : variant === 'secondary' ? colors.secondary : undefined
  const foreground = variant === 'primary' ? colors.primaryForeground : colors.foreground
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: !!inactive, busy }}
      disabled={inactive}
      {...props}
      style={({ pressed }) => [
        {
          minHeight: 40,
          paddingHorizontal: space.lg,
          borderRadius: radius.lg,
          backgroundColor: background,
          alignItems: 'center',
          justifyContent: 'center',
          flexDirection: 'row',
          gap: space.sm,
          opacity: inactive ? 0.5 : pressed ? 0.8 : 1,
        },
        style,
      ]}
    >
      {busy ? <ActivityIndicator size="small" color={foreground} /> : null}
      <Text weight="medium" style={{ color: foreground }}>
        {label}
      </Text>
    </Pressable>
  )
}
