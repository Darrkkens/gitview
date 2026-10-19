// The layout store seen from React: its snapshot, and how its saving state
// reads for the viewer.

import { useSyncExternalStore } from 'react'
import type { LayoutStore, StoreSnapshot } from './persist'

export function useStore(store: LayoutStore): StoreSnapshot {
  return useSyncExternalStore(store.subscribe, store.getSnapshot)
}

/** saveText: where the layout is, for the viewer. */
export function saveText(st: StoreSnapshot): { text: string; tone: 'ok' | 'busy' | 'warn' | 'error' | 'idle'; title: string } {
  const s = st.state
  switch (s.kind) {
    case 'pending':
    case 'saving':
      return { text: 'Saving…', tone: 'busy', title: st.live ? 'Saving to GitView' : 'Saving in this browser' }
    case 'saved':
      return s.where === 'server'
        ? { text: 'Layout saved', tone: 'ok', title: "Saved by gitview serve in GitView's cache (not in the repository)" }
        : { text: 'Saved in this browser', tone: 'ok', title: 'Kept in this browser for this page; download it to share it' }
    case 'error':
      return { text: 'Layout not saved', tone: 'error', title: s.message }
    case 'memory':
      return {
        text: 'Not saved',
        tone: 'warn',
        title: 'This browser keeps nothing for this page: download the layout to keep it',
      }
    default:
      return st.source === 'server'
        ? { text: 'Layout saved', tone: 'idle', title: 'The layout saved by gitview serve' }
        : st.source === 'embedded'
          ? { text: 'Embedded layout', tone: 'idle', title: 'The layout exported with this page' }
          : st.source === 'browser'
            ? { text: 'Saved in this browser', tone: 'idle', title: 'Your arrangement of this page, kept in this browser' }
            : { text: 'Automatic layout', tone: 'idle', title: 'Nothing arranged by hand yet' }
  }
}

/** viewName: a view key for the viewer. */
export function viewName(key: string): string {
  if (key === 'repository') return 'whole repository'
  if (key.startsWith('focus:')) return 'focus view'
  return 'this scope'
}
