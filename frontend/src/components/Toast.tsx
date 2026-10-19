// Toasts: short confirmations ("Copied") and errors, bottom-right.

import { useSyncExternalStore } from 'react'

export type ToastTone = 'info' | 'success' | 'warning' | 'error'

interface ToastItem {
  id: number
  message: string
  tone: ToastTone
  action?: { label: string; run: () => void }
}

let items: ToastItem[] = []
let nextID = 1
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((l) => l())

// oxlint-disable-next-line react/only-export-components
export function toast(message: string, opts?: { tone?: ToastTone; action?: ToastItem['action']; ms?: number }) {
  const id = nextID++
  items = [...items, { id, message, tone: opts?.tone ?? 'info', action: opts?.action }].slice(-4)
  emit()
  setTimeout(() => dismiss(id), opts?.ms ?? (opts?.tone === 'error' ? 6000 : 2600))
}

function dismiss(id: number) {
  items = items.filter((t) => t.id !== id)
  emit()
}

const toneClass: Record<ToastTone, string> = {
  info: 'alert-info',
  success: 'alert-success',
  warning: 'alert-warning',
  error: 'alert-error',
}

export function Toaster() {
  const list = useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => items,
  )
  if (!list.length) return null
  return (
    <div className="toast toast-end toast-bottom z-50 mb-6" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} className={'alert alert-soft py-2 text-sm shadow-lg ' + toneClass[t.tone]}>
          <span>{t.message}</span>
          {t.action && (
            <button
              type="button"
              className="btn btn-ghost btn-xs"
              onClick={() => {
                t.action!.run()
                dismiss(t.id)
              }}
            >
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
