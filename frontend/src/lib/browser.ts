// Small browser helpers: clipboard, downloads and debouncing.

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // file:// pages may not get the async clipboard: fall back to a selection.
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    let ok = false
    try {
      ok = document.execCommand('copy')
    } catch {
      ok = false
    }
    ta.remove()
    return ok
  }
}

/** download saves text as a file of the viewer's (no network involved). */
export function download(name: string, text: string, type = 'text/plain') {
  const url = URL.createObjectURL(new Blob([text], { type: type + ';charset=utf-8' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function debounce<A extends unknown[]>(fn: (...a: A) => void, ms: number): (...a: A) => void {
  let t: ReturnType<typeof setTimeout> | undefined
  return (...a: A) => {
    clearTimeout(t)
    t = setTimeout(() => fn(...a), ms)
  }
}

/** isTyping is true while focus is in a text field (keyboard shortcuts skip it). */
export function isTyping(e?: KeyboardEvent): boolean {
  const el = (e?.target as HTMLElement | null) ?? (document.activeElement as HTMLElement | null)
  if (!el) return false
  const tag = el.tagName
  if (tag === 'INPUT') {
    // Checkboxes, radios and buttons take no text: shortcuts keep working.
    const type = (el as HTMLInputElement).type
    return !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'].includes(type)
  }
  return tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}
