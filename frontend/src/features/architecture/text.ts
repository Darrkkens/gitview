// Text measurement for the canvas: box sizes depend on the width of their
// labels, measured once per (font, text) with an offscreen 2D context.

let mctx: CanvasRenderingContext2D | null = null
const widths = new Map<string, number>()
let family = ''

function uiFamily(): string {
  if (!family) {
    const v = getComputedStyle(document.documentElement).getPropertyValue('--font-sans').trim()
    family = v || getComputedStyle(document.body).fontFamily || 'sans-serif'
  }
  return family
}

/** font returns a CSS font shorthand in the UI family. */
export function font(weight: number, size: number): string {
  return weight + ' ' + size + 'px ' + uiFamily()
}

export function textWidth(text: string, f: string): number {
  const key = f + '\u0000' + text
  let w = widths.get(key)
  if (w === undefined) {
    if (!mctx) mctx = document.createElement('canvas').getContext('2d')
    if (!mctx) return text.length * 7
    mctx.font = f
    w = mctx.measureText(text).width
    if (widths.size > 20000) widths.clear()
    widths.set(key, w)
  }
  return w
}

/** fit shortens text with an ellipsis to fit max world units. */
export function fit(text: string, f: string, max: number): string {
  if (!text) return ''
  if (textWidth(text, f) <= max) return text
  let lo = 0,
    hi = text.length
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (textWidth(text.slice(0, mid) + '…', f) <= max) lo = mid
    else hi = mid - 1
  }
  return text.slice(0, lo) + '…'
}

export const TITLE_FONT = () => font(500, 12.5)
export const SUB_FONT = () => font(400, 11)
export const HEAD_FONT = () => font(600, 12)
