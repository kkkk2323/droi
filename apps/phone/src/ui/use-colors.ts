import { useColorScheme } from 'react-native'
import { COLORS, type ColorScheme, type Colors } from './theme'

export function useColorSchemeName(): ColorScheme {
  return useColorScheme() === 'dark' ? 'dark' : 'light'
}

export function useColors(): Colors {
  return COLORS[useColorSchemeName()]
}
