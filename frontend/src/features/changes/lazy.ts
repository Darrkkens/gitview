// Lazy mounting of long diffs: one IntersectionObserver per scroll area
// tells each placeholder when it comes near the viewport, once.

import { createContext, useContext, useEffect, useState, type RefObject } from 'react'

export class LazyRoot {
  private io: IntersectionObserver | null
  private cbs = new Map<Element, () => void>()

  constructor(root: Element | null, margin = '900px 0px') {
    this.io =
      typeof IntersectionObserver === 'undefined'
        ? null
        : new IntersectionObserver(
            (entries) => {
              for (const e of entries) {
                if (!e.isIntersecting) continue
                const cb = this.cbs.get(e.target)
                if (!cb) continue
                this.cbs.delete(e.target)
                this.io?.unobserve(e.target)
                cb()
              }
            },
            { root, rootMargin: margin },
          )
  }

  /** observe calls cb once el is near the viewport; returns a cancel function. */
  observe(el: Element, cb: () => void): () => void {
    if (!this.io) {
      cb()
      return () => {}
    }
    this.cbs.set(el, cb)
    this.io.observe(el)
    return () => {
      this.cbs.delete(el)
      this.io?.unobserve(el)
    }
  }

  disconnect() {
    this.io?.disconnect()
    this.cbs.clear()
  }
}

export const LazyContext = createContext<LazyRoot | null>(null)

/**
 * useNearViewport is true once the element has come near the viewport (or
 * when forced); it never goes back to false.
 */
export function useNearViewport(ref: RefObject<Element | null>, force: boolean): boolean {
  const lazy = useContext(LazyContext)
  const [seen, setSeen] = useState(false)
  // Latch a forced mount (printing, deep link) so it stays mounted after.
  if (force && !seen) setSeen(true)
  useEffect(() => {
    if (seen || force || !lazy || !ref.current) return
    return lazy.observe(ref.current, () => setSeen(true))
  }, [seen, force, lazy, ref])
  return seen || force
}
