// usePrintable makes the open view printable: the app's chrome is hidden,
// the panes stop clipping (print.css) and the light theme is used while
// printing, as the legacy report did.

import { useEffect } from 'react'
import './print.css'

export function usePrintable() {
  useEffect(() => {
    const html = document.documentElement
    html.classList.add('gd-printable')
    let theme: string | undefined
    const before = () => {
      theme = html.dataset.theme
      html.dataset.theme = 'gitview-light'
    }
    const after = () => {
      if (theme) html.dataset.theme = theme
      theme = undefined
    }
    window.addEventListener('beforeprint', before)
    window.addEventListener('afterprint', after)
    return () => {
      after()
      html.classList.remove('gd-printable')
      window.removeEventListener('beforeprint', before)
      window.removeEventListener('afterprint', after)
    }
  }, [])
}
