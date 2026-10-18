// Design tokens for code that draws (canvas): read from the CSS variables
// of the current theme (styles/global.css) and cached until the theme
// changes. Components re-read them by depending on useTheme().

import { currentTheme } from './theme'

let cache = new Map<string, string>()
let cachedFor = ''

/** token returns the value of a CSS custom property, e.g. token('k-package'). */
export function token(name: string): string {
  const t = currentTheme()
  if (t !== cachedFor) {
    cache = new Map()
    cachedFor = t
  }
  let v = cache.get(name)
  if (v === undefined) {
    const prop = name.startsWith('--') ? name : '--' + name
    v = getComputedStyle(document.documentElement).getPropertyValue(prop).trim()
    cache.set(name, v)
  }
  return v
}
