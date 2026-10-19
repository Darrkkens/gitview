// Flame graph of a pprof profile (SVG). Clicking a frame zooms into it;
// frames matching the search are highlighted, and the inspected function's
// frames are outlined.

import { useEffect, useMemo, useRef, useState } from 'react'
import { useTheme } from '../../lib/theme'
import type { FlameNode } from './types'
import { fmtVal, pct } from './util'

const RH = 18
const MAX_DEPTH = 40

const PALETTE = {
  dark: { mine: '#c98543', other: '#a99a5c', rt: '#6c7c8c', text: '#0d1015' },
  light: { mine: '#efab6b', other: '#e0cd88', rt: '#c3cfda', text: '#1b1f24' },
}

interface Box {
  n: FlameNode
  x: number
  w: number
  d: number
}

export function Flame({
  root,
  total,
  unit,
  find,
  self,
  onZoom,
  zoomed,
}: {
  root: FlameNode
  total: number
  unit: string
  find: string
  /** Symbol ID of the inspected function. */
  self: string
  onZoom: (n: FlameNode | null) => void
  zoomed: boolean
}) {
  const [theme] = useTheme()
  const wrap = useRef<HTMLDivElement>(null)
  const [W, setW] = useState(900)
  useEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(() => setW(Math.max(el.clientWidth, 300)))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const boxes = useMemo(() => {
    const out: Box[] = []
    let maxD = 0
    const walk = (n: FlameNode, x: number, d: number, w: number) => {
      if (d > MAX_DEPTH || w < 1) return
      maxD = Math.max(maxD, d)
      out.push({ n, x, w, d })
      let cx = x
      for (const c of n.children ?? []) {
        const cw = (w * c.value) / Math.max(n.value, 1)
        walk(c, cx, d + 1, cw)
        cx += cw
      }
    }
    walk(root, 0, 0, W)
    return { out, maxD }
  }, [root, W])

  const pal = PALETTE[theme]
  const q = find.trim().toLowerCase()
  const H = (boxes.maxD + 1) * RH
  return (
    <div ref={wrap} className="overflow-hidden rounded-box border border-line bg-base-100">
      <svg
        width={W}
        height={H}
        role="img"
        aria-label="Flame graph"
        className="block"
        style={{ fontFamily: 'var(--font-mono)' }}
      >
        {boxes.out.map(({ n, x, w, d }, i) => {
          const hit = q && n.name.toLowerCase().includes(q)
          const fill = hit
            ? 'var(--color-primary)'
            : n.symbol
              ? pal.mine
              : n.name.startsWith('runtime.')
                ? pal.rt
                : pal.other
          const max = Math.floor((w - 8) / 6.6)
          const label = w > 40 ? (n.name.length > max ? n.name.slice(0, Math.max(max - 1, 1)) + '…' : n.name) : ''
          const mine = !!self && n.symbol === self
          return (
            <g
              key={i}
              className="cursor-pointer"
              onClick={() => {
                if (i > 0) onZoom(n)
                else if (zoomed) onZoom(null)
              }}
            >
              <rect
                x={x}
                y={d * RH}
                width={Math.max(w - 0.5, 0.5)}
                height={RH - 1}
                style={{ fill, stroke: mine ? 'var(--fg)' : 'var(--color-base-100)' }}
                strokeWidth={mine ? 1.5 : 1}
              />
              {label && (
                <text
                  x={x + 4}
                  y={d * RH + 13}
                  fontSize={11}
                  style={{ fill: hit ? 'var(--color-primary-content)' : pal.text }}
                  className="pointer-events-none"
                >
                  {label}
                </text>
              )}
              <title>
                {n.name +
                  '\n' +
                  fmtVal(n.value, unit) +
                  ' (' +
                  pct((100 * n.value) / Math.max(total, 1)) +
                  ')' +
                  (n.self ? '\nself ' + fmtVal(n.self, unit) : '')}
              </title>
            </g>
          )
        })}
      </svg>
    </div>
  )
}

export const FLAME_LEGEND = PALETTE
