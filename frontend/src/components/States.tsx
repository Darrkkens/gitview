import type { ReactNode } from 'react'
import type { SectionName } from '../lib/envelope'
import { useSection, type SectionState } from '../lib/data'
import { Icon, type IconName } from './Icon'

export function Loading({ label }: { label?: string }) {
  return (
    <div className="flex h-full min-h-40 flex-col items-center justify-center gap-3 text-fg-2">
      <span className="loading loading-spinner loading-md text-primary" />
      <span className="text-sm">{label ?? 'Loading…'}</span>
    </div>
  )
}

export function ErrorState({ title, message, action }: { title?: string; message: string; action?: ReactNode }) {
  return (
    <div className="flex h-full min-h-40 items-center justify-center p-6">
      <div role="alert" className="alert alert-error alert-soft max-w-2xl items-start">
        <Icon name="error" className="size-5" />
        <div className="min-w-0">
          <h3 className="font-semibold">{title ?? 'Something went wrong'}</h3>
          <p className="text-sm break-words whitespace-pre-wrap">{message}</p>
          {action && <div className="mt-2">{action}</div>}
        </div>
      </div>
    </div>
  )
}

export function EmptyState({
  icon,
  title,
  children,
  action,
}: {
  icon?: IconName
  title: string
  children?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="flex h-full min-h-40 flex-col items-center justify-center gap-2 p-8 text-center">
      {icon && <Icon name={icon} className="size-8 text-fg-3" />}
      <h3 className="text-base font-semibold">{title}</h3>
      {children && <div className="max-w-lg text-sm text-fg-2">{children}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

/**
 * SectionGate renders its children with a section's data once it is ready,
 * and a loading, error or absent state otherwise.
 */
export function SectionGate<T>({
  section,
  loadingLabel,
  children,
}: {
  section: SectionName
  loadingLabel?: string
  children: (data: T) => ReactNode
}) {
  const s: SectionState<T> = useSection<T>(section)
  switch (s.status) {
    case 'ready':
      return <>{children(s.data)}</>
    case 'loading':
      return <Loading label={loadingLabel ?? 'Analysing the repository…'} />
    case 'error':
      return <ErrorState title="Analysis failed" message={s.error} />
    default:
      return (
        <EmptyState icon="info" title="Not in this report">
          This page was exported without this section.
        </EmptyState>
      )
  }
}
