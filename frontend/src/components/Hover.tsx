// HoverLayer shows what a Go identifier is when the pointer rests on it in
// a code view, as an editor does: its declaration, signature and
// documentation (lib/hover). Code views opt in by marking their Go file
// (data-hover-file, repository-relative) and the line of each code cell
// (data-hl); one listener serves every view, so large diffs cost nothing.

import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { CodeInline } from './Code'
import { utf8Length } from '../lib/format'
import { hoverAt, inspectorHas, type HoverInfo, type HoverReply } from '../lib/hover'
import { routeHref } from '../lib/router'

interface Target {
  key: string
  file: string
  line: number
  col: number
  name: string
  rect: DOMRect
}

interface Shown {
  target: Target
  reply: HoverReply
}

const WORD = /[\p{L}\p{Nd}_]/u
const START = /[\p{L}_]/u
// Comments, strings, numbers and keywords: Chroma classes (c*, s*, m*, k
// but not kt/kc: types and constants) and lib/go's.
const SKIP = /(^|\s)(c\w*|s\w*|m\w*|k[dnpr]?|tk-com|tk-str|tk-num|tk-kw)(\s|$)/

const DWELL = 350
const LINGER = 200

function caretAt(x: number, y: number): { node: Node; offset: number } | null {
  const d = document as Document & {
    caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null
    caretRangeFromPoint?: (x: number, y: number) => Range | null
  }
  if (d.caretPositionFromPoint) {
    const p = d.caretPositionFromPoint(x, y)
    return p ? { node: p.offsetNode, offset: p.offset } : null
  }
  const r = d.caretRangeFromPoint?.(x, y)
  return r ? { node: r.startContainer, offset: r.startOffset } : null
}

/** targetAt finds the Go identifier under the pointer in a marked code cell. */
function targetAt(x: number, y: number): Target | null {
  const pos = caretAt(x, y)
  if (!pos || pos.node.nodeType !== Node.TEXT_NODE) return null
  const node = pos.node as Text
  const el = node.parentElement
  const cell = el?.closest<HTMLElement>('[data-hl]')
  const box = cell?.closest<HTMLElement>('[data-hover-file]')
  if (!el || !cell || !box) return null
  if (el !== cell && SKIP.test(el.className)) return null
  const text = node.data
  let s = Math.min(pos.offset, text.length)
  let e = s
  while (s > 0 && WORD.test(text[s - 1])) s--
  while (e < text.length && WORD.test(text[e])) e++
  if (s === e || !START.test(text[s])) return null
  // The caret snaps to the nearest character: the pointer must be on the word.
  const range = document.createRange()
  range.setStart(node, s)
  range.setEnd(node, e)
  const rect = range.getBoundingClientRect()
  if (x < rect.left - 1 || x > rect.right + 1 || y < rect.top - 1 || y > rect.bottom + 1) return null
  let prefix = ''
  const walk = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT)
  for (let n = walk.nextNode(); n && n !== node; n = walk.nextNode()) prefix += (n as Text).data
  prefix += text.slice(0, s)
  const file = box.dataset.hoverFile!
  const line = Number(cell.dataset.hl)
  const col = utf8Length(prefix) + 1
  const name = text.slice(s, e)
  return { key: file + ':' + line + ':' + col + ':' + name, file, line, col, name, rect }
}

export function HoverLayer() {
  const [shown, setShown] = useState<Shown | null>(null)
  const pop = useRef<HTMLDivElement>(null)
  const cur = useRef<string | null>(null) // key of the word under the pointer
  const dwell = useRef<number | undefined>(undefined)
  const linger = useRef<number | undefined>(undefined)

  useEffect(() => {
    let frame = 0
    let last: MouseEvent | null = null
    const hide = () => {
      window.clearTimeout(dwell.current)
      window.clearTimeout(linger.current)
      cur.current = null
      setShown(null)
    }
    const check = () => {
      frame = 0
      const ev = last
      if (!ev) return
      if (pop.current?.contains(ev.target as Node)) {
        window.clearTimeout(linger.current)
        return
      }
      const t = targetAt(ev.clientX, ev.clientY)
      if (t?.key === cur.current) {
        window.clearTimeout(linger.current)
        return
      }
      window.clearTimeout(dwell.current)
      cur.current = t?.key ?? null
      window.clearTimeout(linger.current)
      linger.current = window.setTimeout(() => setShown((s) => (s && s.target.key !== cur.current ? null : s)), LINGER)
      if (!t) return
      dwell.current = window.setTimeout(() => {
        void hoverAt(t.file, t.line, t.col, t.name).then((reply) => {
          if (cur.current !== t.key) return
          if (reply.status === 'ok' || reply.status === 'stale' || reply.status === 'pending') {
            window.clearTimeout(linger.current)
            setShown({ target: t, reply })
          }
        })
      }, DWELL)
    }
    const move = (ev: MouseEvent) => {
      last = ev
      if (!frame) frame = requestAnimationFrame(check)
    }
    const key = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') hide()
    }
    const scroll = (ev: Event) => {
      if (!pop.current?.contains(ev.target as Node)) hide()
    }
    document.addEventListener('mousemove', move, { passive: true })
    document.addEventListener('keydown', key)
    document.addEventListener('scroll', scroll, { capture: true, passive: true })
    window.addEventListener('blur', hide)
    return () => {
      cancelAnimationFrame(frame)
      window.clearTimeout(dwell.current)
      window.clearTimeout(linger.current)
      document.removeEventListener('mousemove', move)
      document.removeEventListener('keydown', key)
      document.removeEventListener('scroll', scroll, { capture: true })
      window.removeEventListener('blur', hide)
    }
  }, [])

  if (!shown) return null
  const { rect } = shown.target
  const width = Math.min(600, window.innerWidth - 16)
  const below = window.innerHeight - rect.bottom - 8
  const above = rect.top - 8
  const down = below >= 220 || below >= above
  const style = {
    left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)),
    width,
    maxHeight: Math.min(440, down ? below : above),
    ...(down ? { top: rect.bottom + 4 } : { bottom: window.innerHeight - rect.top + 4 }),
  }
  return createPortal(
    <div
      ref={pop}
      role="tooltip"
      className="scroll-thin fixed z-[70] flex flex-col overflow-auto rounded-box border border-line bg-base-100 text-xs shadow-xl print:hidden"
      style={style}
    >
      {shown.reply.info ? (
        <Body info={shown.reply.info} />
      ) : (
        <p className="px-3 py-2 text-fg-2">
          {shown.reply.status === 'pending' ? 'The Go analysis is still running…' : shown.reply.message}
        </p>
      )}
    </div>,
    document.body,
  )
}

function Body({ info }: { info: HoverInfo }) {
  const where = info.file ? info.file + (info.line ? ':' + info.line : '') : ''
  const open = info.local && info.symbol && inspectorHas(info.symbol)
  return (
    <>
      <div className="border-b border-line px-3 py-2">
        <CodeInline code={info.sig} block className="whitespace-pre-wrap" />
      </div>
      {info.decl && (
        <div className="scroll-thin max-h-64 shrink-0 overflow-auto border-b border-line bg-code px-3 py-2">
          <CodeInline code={info.decl} block />
        </div>
      )}
      {info.doc && <Doc text={info.doc} />}
      {(where || open) && (
        <div className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-line px-3 py-1.5 text-[11px] text-fg-3">
          {where && <span className="font-mono break-all">{where}</span>}
          {info.pkg && !info.local && <span className="font-mono">{info.pkg}</span>}
          {open && (
            <a className="link link-hover text-primary" href={routeHref('inspect', { fn: info.symbol })}>
              Open in the Inspector
            </a>
          )}
        </div>
      )}
    </>
  )
}

/**
 * Doc renders a Go doc comment: paragraphs are reflowed, indented blocks
 * stay verbatim (code), list items keep their lines.
 */
function Doc({ text }: { text: string }) {
  const parts: ReactNode[] = []
  text.split(/\n\s*\n/).forEach((para, i) => {
    const lines = para.split('\n')
    if (lines.every((l) => /^\s/.test(l) || !l)) {
      parts.push(
        <pre key={i} className="scroll-thin overflow-x-auto rounded bg-code px-2 py-1 font-mono text-[11px]">
          {lines.map((l) => l.replace(/^\t/, '')).join('\n')}
        </pre>,
      )
    } else if (lines.some((l) => /^\s*([-*•]|\d+[.)])\s/.test(l))) {
      parts.push(
        <p key={i} className="whitespace-pre-wrap">
          {para}
        </p>,
      )
    } else {
      parts.push(<p key={i}>{lines.join(' ')}</p>)
    }
  })
  return <div className="space-y-2 px-3 py-2 leading-relaxed text-fg-2">{parts}</div>
}
