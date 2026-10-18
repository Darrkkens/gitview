// Flame graph of a profile: callers on top, callees below, each frame as
// wide as its share of the samples. Clicking a frame zooms into it; the
// breadcrumb zooms back out. Module functions are coloured.

import { useMemo, useState } from 'react'
import { cx } from '../../lib/format'
import type { FlameNode } from './types'

interface Box {
  n: FlameNode
  x: number // fraction of the view
  w: number
  d: number
  chain: FlameNode[] // ancestors below the focus
}

const ROW = 19
const MODULE_BG = 'color-mix(in oklab, var(--k-unresolved) 50%, var(--color-base-100))'
const EXT_BG = 'color-mix(in oklab, var(--fg-3) 30%, var(--color-base-100))'

export function Flame({ root, fmt }: { root: FlameNode; fmt: (v: number) => string }) {
  const [path, setPath] = useState<FlameNode[]>([])
  const [hover, setHover] = useState<Box | null>(null)
  const focus = path.length ? path[path.length - 1] : root
  const total = root.value || 1

  const { boxes, depth } = useMemo(() => {
    const out: Box[] = []
    let depth = 0
    const tot = focus.value || 1
    const walk = (n: FlameNode, x0: number, d: number, chain: FlameNode[]) => {
      let x = x0
      for (const c of n.children ?? []) {
        const w = c.value / tot
        if (w >= 0.0015) {
          out.push({ n: c, x, w, d, chain })
          if (d + 1 > depth) depth = d + 1
          walk(c, x, d + 1, [...chain, c])
        }
        x += w
      }
    }
    walk(focus, 0, 0, [])
    return { boxes: out, depth }
  }, [focus])

  const shown = hover ?? null
  return (
    <div>
      <div className="mb-1 flex min-h-6 flex-wrap items-center gap-1 text-[11px]">
        <button
          type="button"
          className={cx('btn btn-xs', path.length ? 'btn-ghost' : 'btn-disabled btn-ghost')}
          onClick={() => setPath([])}
        >
          reset zoom
        </button>
        <span className="text-fg-3">all</span>
        {path.map((p, i) => (
          <span key={i} className="flex items-center gap-1">
            <span className="text-fg-3">›</span>
            <button
              type="button"
              className="max-w-56 truncate font-mono text-accent-text hover:underline"
              title={p.name}
              onClick={() => setPath(path.slice(0, i + 1))}
            >
              {p.name}
            </button>
          </span>
        ))}
      </div>
      <div
        className="relative overflow-hidden rounded-field border border-line bg-base-200"
        style={{ height: Math.max(depth, 1) * ROW + 4 }}
        onMouseLeave={() => setHover(null)}
      >
        {boxes.map((b, i) => (
          <div
            key={i}
            role="button"
            tabIndex={-1}
            onClick={() => setPath([...path, ...b.chain, b.n].filter((n, k, a) => a.indexOf(n) === k))}
            onMouseEnter={() => setHover(b)}
            className={cx(
              'absolute cursor-pointer truncate border-r border-b border-base-100 px-1 font-mono text-[10.5px] leading-[18px] text-fg hover:brightness-110',
              hover?.n === b.n && 'outline outline-1 outline-primary',
            )}
            style={{
              left: b.x * 100 + '%',
              width: b.w * 100 + '%',
              top: b.d * ROW,
              height: ROW - 1,
              background: b.n.symbol ? MODULE_BG : EXT_BG,
            }}
          >
            {b.w > 0.02 ? b.n.name + ' ' + ((100 * b.n.value) / (focus.value || 1)).toFixed(1) + '%' : ''}
          </div>
        ))}
        {!boxes.length && <div className="p-3 text-xs text-fg-3">No samples below this frame.</div>}
      </div>
      <div className="mt-1 min-h-4 truncate font-mono text-[11px] text-fg-2">
        {shown ? (
          <>
            {shown.n.name} · {fmt(shown.n.value)} · {((100 * shown.n.value) / total).toFixed(2)}% of the profile
            {focus !== root && ' · ' + ((100 * shown.n.value) / (focus.value || 1)).toFixed(2) + '% of the view'}
            {shown.n.symbol ? ' · module function' : ''}
          </>
        ) : (
          <span className="font-sans text-fg-3">Hover a frame for its share; click to zoom.</span>
        )}
      </div>
    </div>
  )
}
