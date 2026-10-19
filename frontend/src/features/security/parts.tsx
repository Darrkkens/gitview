// Small parts of the Security Journey: severity and state badges (a state
// is always written next to its colour), the AI tag and command lines.

import type { ReactNode } from 'react'
import { CopyButton } from '../../components/CopyButton'
import { cx } from '../../lib/format'
import type { FState, StateClass } from './model'

const SEV_CLASS: Record<string, string> = {
  critical: 'text-sev-critical bg-sev-critical/16',
  high: 'text-sev-high bg-sev-high/14',
  medium: 'text-sev-medium bg-sev-medium/14',
  low: 'text-sev-low bg-sev-low/14',
  info: 'text-sev-info bg-sev-info/14',
}

export function Sev({ sev, className }: { sev: string; className?: string }) {
  return (
    <span
      className={cx(
        'inline-flex h-[18px] shrink-0 items-center rounded px-1.5 text-[10px] font-semibold tracking-wide uppercase print:border print:border-current',
        SEV_CLASS[sev] ?? SEV_CLASS.info,
        className,
      )}
    >
      {sev}
    </span>
  )
}

const DOT_CLASS: Record<StateClass, string> = {
  vulnerable: 'bg-s-vuln',
  review: 'bg-s-review',
  incomplete: 'bg-s-unres',
  check: 'bg-s-check',
  struct: 'bg-s-struct',
  dismissed: 'border-2 border-s-unres bg-transparent',
}

export function SDot({ cls, title, className }: { cls: StateClass; title?: string; className?: string }) {
  return <span className={cx('inline-block size-2.5 shrink-0 rounded-full', DOT_CLASS[cls], className)} title={title} />
}

const STATE_TEXT: Record<StateClass, string> = {
  vulnerable: 'text-s-vuln border-s-vuln/45 bg-s-vuln/8',
  review: 'text-s-review border-s-review/45 bg-s-review/8',
  incomplete: 'text-s-unres border-line-strong bg-surface-2',
  check: 'text-s-check border-s-check/45 bg-s-check/8',
  struct: 'text-s-struct border-s-struct/45 bg-s-struct/8',
  dismissed: 'text-fg-3 border-line bg-surface-2',
}

/** StateBadge: the state of a finding, colour and text. */
export function StateBadge({ st, className }: { st: FState; className?: string }) {
  return (
    <span className={cx('badge badge-sm shrink-0 gap-1.5 border', STATE_TEXT[st.cls], className)}>
      <SDot cls={st.cls} className="size-2" />
      {st.label}
    </span>
  )
}

export function Verdict({ st, children }: { st: FState; children: ReactNode }) {
  return (
    <div
      className={cx(
        'mb-2 flex items-center gap-2 rounded-box border px-2.5 py-2 text-xs font-semibold',
        STATE_TEXT[st.cls],
      )}
    >
      <SDot cls={st.cls} />
      <span className="min-w-0">{children}</span>
    </div>
  )
}

export function AiTag({ children = 'AI', className }: { children?: ReactNode; className?: string }) {
  return (
    <span
      className={cx(
        'inline-flex h-[18px] shrink-0 items-center rounded px-1.5 text-[10px] font-semibold text-s-ai bg-s-ai/14',
        className,
      )}
    >
      {children}
    </span>
  )
}

export function Cmd({ text }: { text: string }) {
  return (
    <div className="mt-1.5 flex items-center gap-1">
      <code className="scroll-thin min-w-0 flex-1 overflow-x-auto rounded-field border border-line bg-code px-2 py-1 font-mono text-[11px] whitespace-nowrap">
        {text}
      </code>
      <CopyButton text={text} title="Copy the command" />
    </div>
  )
}

const ROLE_CLASS: Record<string, string> = {
  source: 'text-s-vuln',
  sink: 'text-s-vuln',
  sanitizer: 'text-s-check',
  check: 'text-s-check',
  validation: 'text-s-review',
  absence: 'text-s-unres',
}

/** Role: an evidence kind or a path role, as a small caption. */
export function Role({ role, className }: { role: string; className?: string }) {
  return (
    <span
      className={cx(
        'shrink-0 text-[10px] leading-4 font-semibold tracking-wide uppercase',
        ROLE_CLASS[role] ?? 'text-fg-3',
        className,
      )}
    >
      {role}
    </span>
  )
}
