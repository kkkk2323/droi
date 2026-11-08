// The words the hooks report a Session's state in, which the Clients branch on.

export const LOAD_STATE = {
  notLoaded: 'NOT_LOADED',
  loading: 'LOADING',
  loaded: 'LOADED',
} as const

export type LoadState = (typeof LOAD_STATE)[keyof typeof LOAD_STATE]
