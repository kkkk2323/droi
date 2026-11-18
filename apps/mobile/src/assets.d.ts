// Fonts the app has no module for: Metro hands a bundled file's asset id to
// useFonts, which expo-font takes in place of a module URL.
declare module '*.ttf' {
  const asset: number
  export default asset
}
