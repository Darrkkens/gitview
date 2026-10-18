// Calls between layers, drawn as an arc diagram: the layers stacked in
// their display order, calls going down the stack on the right, calls
// going up on the left, one arc per pair with its number of calls. Hovering
// a layer or an arc highlights it; an arc opens its example call.

import { useMemo, useState } from 'react'
import { loc, plural } from '../../../lib/format'
import { useStudy } from '../context'
import { layerColor } from '../fmt'
import { track } from '../progress'
import type { LayerEdge } from '../types'

const NODE_W = 168
const NODE_H = 28
const GAP = 22
const STEP = 34 // horizontal reach of an arc per layer it spans

/** Width of the count pill of an arc. */
const pill = (n: number) => 9 + String(n).length * 6.2

interface Arc {
  e: LayerEdge
  d: string
  lx: number
  ly: number
  width: number
  href: string | null
}

export function LayerGraph({ edges, order }: { edges: LayerEdge[]; order: string[] }) {
  const { links } = useStudy()
  const [hover, setHover] = useState<string | null>(null) // layer or "from→to"

  const g = useMemo(() => {
    const present = new Set<string>()
    for (const e of edges) {
      present.add(e.from)
      present.add(e.to)
    }
    const nodes = order.filter((l) => present.has(l))
    for (const l of present) if (!nodes.includes(l)) nodes.push(l)
    const rank = new Map(nodes.map((l, i) => [l, i]))
    let down = 0
    let up = 0
    for (const e of edges) {
      const span = (rank.get(e.to) ?? 0) - (rank.get(e.from) ?? 0)
      if (span >= 0) down = Math.max(down, span || 1)
      else up = Math.max(up, -span)
    }
    const max = Math.max(...edges.map((e) => e.count), 1)
    const left = 16 + (up ? up * STEP + 24 : 0)
    const width = left + NODE_W + 16 + (down ? down * STEP + 32 : 0)
    const height = nodes.length * (NODE_H + GAP) - GAP + 16
    const y = (l: string) => 8 + (rank.get(l) ?? 0) * (NODE_H + GAP) + NODE_H / 2
    const arcs: Arc[] = edges.map((e, i) => {
      const r0 = rank.get(e.from) ?? 0
      const r1 = rank.get(e.to) ?? 0
      const span = r1 - r0
      const w = 1.2 + Math.min(3, Math.log2(e.count + 1) * 0.6)
      const href = links.href(e.example.ref)
      // Arcs leaving and entering the same side are spread a little apart.
      const jitter = ((i % 3) - 1) * 3
      if (span >= 0) {
        const x0 = left + NODE_W
        const reach = (span || 1) * STEP + 14
        const y0 = y(e.from) + (span === 0 ? -6 : 4) + jitter
        const y1 = y(e.to) - (span === 0 ? -6 : 4) + jitter
        const d = `M${x0} ${y0} C${x0 + reach} ${y0} ${x0 + reach} ${y1} ${x0 + 3} ${y1}`
        return { e, d, lx: x0 + reach * 0.75, ly: (y0 + y1) / 2, width: w, href }
      }
      const x0 = left
      const reach = -span * STEP + 14
      const y0 = y(e.from) - 4 + jitter
      const y1 = y(e.to) + 4 + jitter
      const d = `M${x0} ${y0} C${x0 - reach} ${y0} ${x0 - reach} ${y1} ${x0 - 3} ${y1}`
      return { e, d, lx: x0 - reach * 0.75, ly: (y0 + y1) / 2, width: w, href }
    })
    return { nodes, arcs, width, height, left, y, max }
  }, [edges, order, links])

  const isOn = (e: LayerEdge) => !hover || hover === e.from || hover === e.to || hover === e.from + '→' + e.to

  return (
    <div className="scroll-thin overflow-x-auto rounded-box border border-line bg-canvas p-2">
      <svg
        width={g.width}
        height={g.height}
        viewBox={`0 0 ${g.width} ${g.height}`}
        className="mx-auto block max-w-none"
        role="img"
        aria-label="Calls between layers"
      >
        <defs>
          <marker
            id="study-arrow"
            viewBox="0 0 8 8"
            refX="7"
            refY="4"
            markerWidth="8"
            markerHeight="8"
            markerUnits="userSpaceOnUse"
            orient="auto-start-reverse"
          >
            <path d="M0 0L8 4L0 8z" style={{ fill: 'var(--edge-strong)' }} />
          </marker>
          <marker
            id="study-arrow-hl"
            viewBox="0 0 8 8"
            refX="7"
            refY="4"
            markerWidth="8"
            markerHeight="8"
            markerUnits="userSpaceOnUse"
            orient="auto-start-reverse"
          >
            <path d="M0 0L8 4L0 8z" style={{ fill: 'var(--hl-out)' }} />
          </marker>
        </defs>
        {(['line', 'pill'] as const).map((part) =>
          g.arcs.map((a, i) => {
            const on = isOn(a.e)
            const hl = !!hover && on
            const key = a.e.from + '→' + a.e.to
            const title = `${a.e.from} → ${a.e.to}: ${plural(a.e.count, 'call')}\nexample: ${a.e.example.text}${loc(a.e.example) ? ' (' + loc(a.e.example) + ')' : ''}`
            const body = (
              <g
                onMouseEnter={() => setHover(key)}
                onMouseLeave={() => setHover(null)}
                style={{ opacity: on ? 1 : 0.18, cursor: a.href ? 'pointer' : 'default' }}
              >
                <title>{title}</title>
                {part === 'line' ? (
                  <>
                    <path d={a.d} fill="none" stroke="transparent" strokeWidth={10} />
                    <path
                      d={a.d}
                      fill="none"
                      style={{ stroke: hl ? 'var(--hl-out)' : 'var(--edge)' }}
                      strokeWidth={a.width}
                      markerEnd={hl ? 'url(#study-arrow-hl)' : 'url(#study-arrow)'}
                    />
                  </>
                ) : (
                  <g transform={`translate(${a.lx} ${a.ly})`}>
                    <rect
                      x={-pill(a.e.count) / 2}
                      y={-7.5}
                      width={pill(a.e.count)}
                      height={15}
                      rx={7.5}
                      style={{ fill: 'var(--canvas)', stroke: hl ? 'var(--hl-out)' : 'var(--edge)' }}
                    />
                    <text
                      y={3.5}
                      textAnchor="middle"
                      style={{ fill: hl ? 'var(--hl-out)' : 'var(--fg-2)', fontSize: 10 }}
                      className="font-mono"
                    >
                      {a.e.count}
                    </text>
                  </g>
                )}
              </g>
            )
            return a.href ? (
              <a key={part + i} href={a.href} onClick={() => track('architecture', 'layer:' + a.e.from)}>
                {body}
              </a>
            ) : (
              <g key={part + i}>{body}</g>
            )
          }),
        )}
        {g.nodes.map((l) => {
          const y = g.y(l) - NODE_H / 2
          const dim = hover && hover !== l && !g.arcs.some((a) => isOn(a.e) && (a.e.from === l || a.e.to === l))
          return (
            <g
              key={l}
              transform={`translate(${g.left} ${y})`}
              onMouseEnter={() => setHover(l)}
              onMouseLeave={() => setHover(null)}
              onClick={() => track('architecture', 'layer:' + l)}
              style={{ opacity: dim ? 0.35 : 1, cursor: 'default' }}
            >
              <title>{l}</title>
              <rect
                width={NODE_W}
                height={NODE_H}
                rx={6}
                style={{ fill: 'var(--node-fill)', stroke: hover === l ? 'var(--hl-out)' : 'var(--node-border)' }}
                strokeWidth={hover === l ? 1.6 : 1}
              />
              <rect x={0} y={0} width={4} height={NODE_H} rx={2} style={{ fill: layerColor(l) }} />
              <text x={14} y={NODE_H / 2 + 4} style={{ fill: 'var(--fg)', fontSize: 12, fontWeight: 600 }}>
                {l}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
