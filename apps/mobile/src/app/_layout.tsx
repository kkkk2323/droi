// The Phone App's root: loads fonts and app storage before the first screen,
// then a stack whose main screen is the Session (or pairing) and whose
// settings and pairing pages are pushed on top.
import '../polyfills'
import AsyncStorage from '@react-native-async-storage/async-storage'
import { setPreferenceStorage } from '@droi/daemon-layer/local-preference'
import { Geist_400Regular, Geist_500Medium, Geist_600SemiBold } from '@expo-google-fonts/geist'
import { GeistMono_400Regular, GeistMono_500Medium } from '@expo-google-fonts/geist-mono'
import { QueryClientProvider } from '@tanstack/react-query'
import { useFonts } from 'expo-font'
import { Stack } from 'expo-router'
import * as SplashScreen from 'expo-splash-screen'
import { StatusBar } from 'expo-status-bar'
import { Suspense, use } from 'react'
import { GestureHandlerRootView } from 'react-native-gesture-handler'
import { hydrateStorage } from '../lib/app-storage'
import { appQueryClient } from '../query-client'
import { fonts } from '../ui/theme'
import { applyThemeChoice, themeChoice, useColorSchemeName, useColors } from '../ui/use-colors'

void SplashScreen.preventAutoHideAsync()

const storageReady = hydrateStorage(AsyncStorage).then((storage) => {
  setPreferenceStorage(storage)
  applyThemeChoice()
  themeChoice.subscribe(() => applyThemeChoice())
})

export default function RootLayout() {
  return (
    <Suspense fallback={null}>
      <Root />
    </Suspense>
  )
}

function Root() {
  use(storageReady)
  const [fontsLoaded, fontError] = useFonts({
    Geist_400Regular,
    Geist_500Medium,
    Geist_600SemiBold,
    GeistMono_400Regular,
    GeistMono_500Medium,
  })
  const colors = useColors()
  const scheme = useColorSchemeName()

  if (!fontsLoaded && !fontError) return null
  void SplashScreen.hideAsync()

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={appQueryClient}>
        <StatusBar style={scheme === 'dark' ? 'light' : 'dark'} />
        <Stack
          screenOptions={{
            headerStyle: { backgroundColor: colors.background },
            headerTintColor: colors.foreground,
            headerTitleStyle: { fontFamily: fonts.sansSemiBold, fontSize: 17 },
            headerShadowVisible: false,
            contentStyle: { backgroundColor: colors.background },
          }}
        >
          <Stack.Screen name="index" options={{ headerShown: false }} />
          <Stack.Screen name="settings" options={{ title: 'Settings' }} />
          <Stack.Screen name="session-defaults" options={{ title: 'Session defaults' }} />
          <Stack.Screen name="pair" options={{ title: 'Add a computer' }} />
          <Stack.Screen name="scan" options={{ title: 'Scan QR code' }} />
          <Stack.Screen name="computers" options={{ title: 'Paired computers' }} />
          <Stack.Screen name="computer/[id]" options={{ title: 'Computer' }} />
        </Stack>
      </QueryClientProvider>
    </GestureHandlerRootView>
  )
}
