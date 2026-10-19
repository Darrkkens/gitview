// The Changes view: the report's overview (summary, warnings), the list of
// changed files and their diffs, with the legacy report's filter, expand /
// collapse, copy and keyboard navigation, deep links and print support.

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { flushSync } from 'react-dom'
import { EmptyState } from '../../components/States'
import { Icon } from '../../components/Icon'
import { Panes } from '../../components/Panes'
import { StatusSlot } from '../../components/Slots'
import { toast } from '../../components/Toast'
import { isTyping } from '../../lib/browser'
import { cx } from '../../lib/format'
import { setParams } from '../../lib/router'
import { fileByPath, num } from './diff'
import { FileDiff, type Target } from './FileDiff'
import { FileList } from './FileList'
import { LazyContext, LazyRoot } from './lazy'
import { usePrintable } from './print'
import { Summary } from './Summary'
import type { DiffFile, ReportSection } from './types'

/** Height of the sticky toolbar: file headers stick right below it. */
const TOOLBAR_H = 44

let nonce = 0

// What the reader did here survives a trip to another view (which unmounts
// this one): filter, expanded files, list, scroll position — per report, in
// memory only.
interface Memory {
  report: ReportSection
  filter: string
  open: Record<string, boolean>
  listPref: boolean | null
  current: string | null
  scrollTop: number
}
let memory: Memory | null = null

function useNarrow(px: number): boolean {
  const [narrow, setNarrow] = useState(() => window.innerWidth < px)
  useEffect(() => {
    const on = () => setNarrow(window.innerWidth < px)
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [px])
  return narrow
}

export function Changes({ report, params }: { report: ReportSection; params: URLSearchParams }) {
  const files = report.files
  const [mem] = useState(() => (memory?.report === report ? memory : null))
  const [filter, setFilter] = useState(mem?.filter ?? '')
  const [open, setOpen] = useState<Record<string, boolean>>(
    () => mem?.open ?? Object.fromEntries(files.map((f) => [f.id, f.open])),
  )
  const [current, setCurrent] = useState<string | null>(mem?.current ?? null)
  const [printing, setPrinting] = useState(false)
  const narrow = useNarrow(900)
  const [listPref, setListPref] = useState<boolean | null>(mem?.listPref ?? null)
  const empty = files.length === 0
  const showList = (listPref ?? !narrow) && !printing && !empty
  const [target, setTarget] = useState<{ fileId: string; t: Target } | null>(null)
  const [reveal, setReveal] = useState<{ fileId: string; nonce: number } | null>(null)

  const scrollRef = useRef<HTMLDivElement>(null)
  const filterRef = useRef<HTMLInputElement>(null)
  const [lazy, setLazy] = useState<LazyRoot | null>(null)
  useLayoutEffect(() => {
    const l = new LazyRoot(scrollRef.current)
    setLazy(l)
    return () => l.disconnect()
  }, [])

  // Remember the state for the next visit; restore the scroll position of
  // the last one (unless a link asks for a file).
  const scrollTop = useRef(mem?.scrollTop ?? 0)
  useEffect(() => {
    memory = { report, filter, open, listPref, current, scrollTop: scrollTop.current }
  })
  useEffect(
    () => () => {
      if (memory?.report === report) memory.scrollTop = scrollTop.current
    },
    [report],
  )
  useLayoutEffect(() => {
    if (mem?.scrollTop && !params.get('file') && scrollRef.current) scrollRef.current.scrollTop = mem.scrollTop
    // only on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const q = filter.trim().toLowerCase()
  const visible = useMemo(() => (q ? files.filter((f) => f.path.toLowerCase().includes(q)) : files), [files, q])

  // Programmatic scrolls must not move "current" through the scroll spy.
  const quietUntil = useRef(0)
  const quiet = () => (quietUntil.current = performance.now() + 700)

  const toggle = useCallback((id: string, to?: boolean) => {
    setOpen((m) => ({ ...m, [id]: to ?? !m[id] }))
    setCurrent(id)
    // Collapsing a long diff whose header is stuck at the top: bring the
    // file back into view instead of leaving the reader further down.
    requestAnimationFrame(() => {
      const el = document.getElementById('gd-' + id)
      const root = scrollRef.current
      if (!el || !root) return
      if (el.getBoundingClientRect().top < root.getBoundingClientRect().top + TOOLBAR_H - 1) {
        quiet()
        el.scrollIntoView({ block: 'start' })
      }
    })
  }, [])

  const setAll = useCallback(
    (to: boolean) =>
      setOpen((m) => {
        const next = { ...m }
        for (const f of visible) next[f.id] = to
        return next
      }),
    [visible],
  )

  // Focus a file (and a line): open it, clear a filter hiding it, scroll.
  const focus = useCallback(
    (f: DiffFile, line: number, scroll: boolean) => {
      if (q && !f.path.toLowerCase().includes(q)) setFilter('')
      setOpen((m) => (m[f.id] ? m : { ...m, [f.id]: true }))
      setCurrent(f.id)
      quiet()
      if (line > 0) {
        setTarget({ fileId: f.id, t: { line, scroll, nonce: ++nonce } })
        if (f.omitted || f.binary) setReveal({ fileId: f.id, nonce: ++nonce })
      } else {
        setTarget(null)
        if (scroll) setReveal({ fileId: f.id, nonce: ++nonce })
      }
    },
    [q],
  )

  // Deep links: #/changes?file=<path>&line=<n>.
  const fileKey = params.get('file') ?? ''
  const lineKey = Number(params.get('line')) || 0
  const selfNav = useRef('')
  useEffect(() => {
    if (!fileKey) return
    const f = fileByPath(report, fileKey)
    if (!f) {
      toast(`${fileKey} is not among the changed files of this report.`, { tone: 'warning' })
      return
    }
    const self = selfNav.current === fileKey + ':' + lineKey
    selfNav.current = ''
    if (!self) focus(f, lineKey, true)
    // focus is recreated when the filter changes: only the link matters here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileKey, lineKey, report])

  // Links made here update the URL (shareable) and act at once; the effect
  // above then skips them.
  const link = useRef({ file: fileKey, line: lineKey })
  useEffect(() => {
    link.current = { file: fileKey, line: lineKey }
  }, [fileKey, lineKey])
  const linkTo = useCallback(
    (f: DiffFile, line: number, scroll: boolean) => {
      if (f.path !== link.current.file || line !== link.current.line) {
        selfNav.current = f.path + ':' + line
        setParams({ file: f.path, line: line || undefined })
      }
      focus(f, line, scroll)
    },
    [focus],
  )
  const openFile = useCallback((f: DiffFile) => linkTo(f, 0, true), [linkTo])
  const pickLine = useCallback((f: DiffFile, line: number) => linkTo(f, line, false), [linkTo])

  // Scroll spy: the file under the toolbar is the current one.
  useEffect(() => {
    const root = scrollRef.current
    if (!root) return
    let raf = 0
    const spy = () => {
      raf = 0
      if (performance.now() < quietUntil.current) return
      const top = root.getBoundingClientRect().top + TOOLBAR_H + 8
      let id: string | null = null
      for (const el of root.querySelectorAll<HTMLElement>('section[data-file]')) {
        if (el.getBoundingClientRect().top <= top) id = el.dataset.file ?? null
        else break
      }
      if (id) setCurrent(id)
    }
    const on = () => {
      scrollTop.current = root.scrollTop
      if (!raf) raf = requestAnimationFrame(spy)
    }
    root.addEventListener('scroll', on, { passive: true })
    return () => {
      root.removeEventListener('scroll', on)
      cancelAnimationFrame(raf)
    }
  }, [])

  // Keyboard: / filter, j/k next/previous file, o toggle, e/c expand/collapse all.
  const step = useCallback(
    (dir: 1 | -1) => {
      if (!visible.length) return
      let pos = visible.findIndex((f) => f.id === current)
      pos = pos < 0 ? (dir > 0 ? 0 : visible.length - 1) : Math.min(Math.max(pos + dir, 0), visible.length - 1)
      const f = visible[pos]
      setCurrent(f.id)
      quiet()
      const el = document.getElementById('gd-' + f.id)
      el?.scrollIntoView({ block: 'start' })
      el?.querySelector<HTMLElement>('[data-gd-toggle]')?.focus({ preventScroll: true })
    },
    [visible, current],
  )
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return
      if (isTyping(e)) return
      switch (e.key) {
        case '/':
          e.preventDefault()
          filterRef.current?.focus()
          filterRef.current?.select()
          break
        case 'j':
          step(1)
          break
        case 'k':
          step(-1)
          break
        case 'o': {
          const id = current ?? visible[0]?.id
          if (id) toggle(id)
          break
        }
        case 'e':
          setAll(true)
          break
        case 'c':
          setAll(false)
          break
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [step, toggle, setAll, current, visible])

  // Printing: light theme, no chrome (usePrintable), every file expanded and
  // fully mounted.
  usePrintable()
  useEffect(() => {
    const before = () => flushSync(() => setPrinting(true))
    const after = () => setPrinting(false)
    window.addEventListener('beforeprint', before)
    window.addEventListener('afterprint', after)
    return () => {
      window.removeEventListener('beforeprint', before)
      window.removeEventListener('afterprint', after)
    }
  }, [])

  const openCount = visible.reduce((n, f) => n + (open[f.id] ? 1 : 0), 0)
  const openPath = useCallback(
    (p: string) => {
      const f = fileByPath(report, p)
      if (f) openFile(f)
    },
    [report, openFile],
  )

  return (
    <LazyContext.Provider value={lazy}>
      <div className="h-full print:h-auto">
        <Panes
          id="changes"
          leftWidth={300}
          showLeft={showList}
          left={<FileList files={visible} total={files.length} current={current} onOpen={openFile} />}
        >
          <div
            ref={scrollRef}
            className="gd-scroll scroll-thin h-full overflow-y-auto print:h-auto print:overflow-visible"
            style={{ '--gd-top': TOOLBAR_H + 'px' } as CSSProperties}
          >
            <Summary report={report} onOpenFile={openPath} />
            {!empty && (
              <div
                className="sticky top-0 z-10 flex flex-wrap items-center gap-2 border-y border-line bg-base-200/95 px-4 backdrop-blur-sm sm:px-6 print:hidden"
                style={{ minHeight: TOOLBAR_H }}
              >
                <button
                  type="button"
                  className={cx('btn btn-square btn-ghost btn-sm', showList && 'btn-active')}
                  title={showList ? 'Hide the file list' : 'Show the file list'}
                  aria-pressed={showList}
                  onClick={() => setListPref(!showList)}
                >
                  <Icon name="panelLeft" />
                </button>
                <h2 className="text-sm font-semibold">
                  Files changed <span className="badge ml-1 badge-ghost badge-sm">{num(files.length)}</span>
                </h2>
                <label className="input input-sm ml-auto w-64 max-sm:order-last max-sm:ml-0 max-sm:w-full">
                  <Icon name="filter" className="size-3.5 text-fg-3" />
                  <input
                    ref={filterRef}
                    type="search"
                    placeholder="Filter files…"
                    autoComplete="off"
                    spellCheck={false}
                    aria-label="Filter files"
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') {
                        setFilter('')
                        e.currentTarget.blur()
                      }
                    }}
                  />
                  <kbd className="kbd kbd-xs">/</kbd>
                </label>
                {q && (
                  <span className="text-xs text-fg-2" aria-live="polite">
                    {num(visible.length)} of {num(files.length)}
                  </span>
                )}
                <div className="join max-sm:ml-auto">
                  <button
                    type="button"
                    className="btn join-item btn-sm"
                    onClick={() => setAll(true)}
                    disabled={!visible.length || openCount === visible.length}
                    title="Expand all (e)"
                  >
                    Expand all
                  </button>
                  <button
                    type="button"
                    className="btn join-item btn-sm"
                    onClick={() => setAll(false)}
                    disabled={!openCount}
                    title="Collapse all (c)"
                  >
                    Collapse all
                  </button>
                </div>
                <button
                  type="button"
                  className="btn btn-square btn-ghost btn-sm max-md:hidden"
                  title="Print the diff"
                  onClick={() => window.print()}
                >
                  <Icon name="print" />
                </button>
              </div>
            )}
            <div className="px-3 pt-3 pb-4 sm:px-6">
              {files.length === 0 ? (
                <EmptyState icon="diff" title="No changes in this report">
                  The working tree matches {report.base ? 'the base commit' : 'an empty repository'}.
                </EmptyState>
              ) : visible.length === 0 ? (
                <EmptyState
                  icon="filter"
                  title="No files match the filter"
                  action={
                    <button type="button" className="btn btn-sm" onClick={() => setFilter('')}>
                      Clear the filter
                    </button>
                  }
                />
              ) : (
                visible.map((f) => (
                  <FileDiff
                    key={f.id}
                    file={f}
                    open={printing || !!open[f.id]}
                    current={f.id === current}
                    mountAll={printing}
                    target={target?.fileId === f.id ? target.t : undefined}
                    reveal={reveal?.fileId === f.id ? reveal.nonce : undefined}
                    onToggle={toggle}
                    onPickLine={pickLine}
                  />
                ))
              )}
              {!empty && (
                <p className="mt-6 text-center text-xs text-fg-3 max-md:hidden print:hidden">
                  Keys: <kbd className="kbd kbd-xs">/</kbd> filter · <kbd className="kbd kbd-xs">j</kbd>/
                  <kbd className="kbd kbd-xs">k</kbd> next/previous file · <kbd className="kbd kbd-xs">o</kbd> toggle ·{' '}
                  <kbd className="kbd kbd-xs">e</kbd>/<kbd className="kbd kbd-xs">c</kbd> expand/collapse all
                </p>
              )}
            </div>
          </div>
        </Panes>
      </div>
      <StatusSlot>
        <span>
          {num(files.length)} {files.length === 1 ? 'file' : 'files'} changed
        </span>
        <span>
          <span className="text-success">+{num(report.added)}</span>{' '}
          <span className="text-error">−{num(report.deleted)}</span>
        </span>
        {q && <span>{num(visible.length)} shown</span>}
      </StatusSlot>
    </LazyContext.Provider>
  )
}
