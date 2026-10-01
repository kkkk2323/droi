// How every native stack looks: the root one and the connected computer's.
import type { ComponentProps } from 'react'
import type { Stack } from 'expo-router'
import { fonts } from './theme'
import { useColors } from './use-colors'

type ScreenOptions = NonNullable<ComponentProps<typeof Stack>['screenOptions']>

export function useStackScreenOptions(): Exclude<ScreenOptions, (...args: never[]) => unknown> {
  const colors = useColors()
  return {
    headerStyle: { backgroundColor: colors.background },
    headerTintColor: colors.foreground,
    headerTitleStyle: { fontFamily: fonts.sansSemiBold, fontSize: 17 },
    headerShadowVisible: false,
    headerBackButtonDisplayMode: 'minimal',
    contentStyle: { backgroundColor: colors.background },
  }
}
