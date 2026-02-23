import 'csstype'

declare module 'csstype' {
  interface Properties<TLength = (string & {}) | 0, TTime = string & {}> {
    WebkitAppRegion?: 'drag' | 'no-drag'
  }
}

export {}
