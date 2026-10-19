import { envelope, isLive } from '../lib/envelope'
import { offered, reloadLoaded, usePeek, useServerStatus } from '../lib/data'
import { cx } from '../lib/format'
import { routeHref } from '../lib/router'
import { useTheme } from '../lib/theme'
import type { ViewDef } from '../views'
import { Icon, Logo } from './Icon'

function ViewTab({ view, active }: { view: ViewDef; active: boolean }) {
  const s = usePeek<unknown>(view.section)
  if (!offered(view.section)) return null
  if (s?.status === 'ready' && view.available && !view.available(s.data)) return null
  // Nine views do not fit next to a view's own header controls: below
  // 1680px the inactive tabs show only their icon (named by title/aria).
  return (
    <a
      role="tab"
      href={routeHref(view.id)}
      aria-selected={active}
      aria-label={view.label}
      title={active ? undefined : view.label}
      className={cx('tab shrink-0 gap-1.5 px-2.5 text-[13px]', active && 'tab-active')}
    >
      <Icon name={view.icon} className="size-3.5 opacity-80" />
      <span className={active ? '' : 'hidden min-[1680px]:inline'}>{view.label}</span>
    </a>
  )
}

function LiveState() {
  const { status, changedAt } = useServerStatus()
  if (!isLive()) return null
  const running = Object.entries(status?.sections ?? {}).filter(([, s]) => s?.state === 'running')
  return (
    <div className="flex items-center gap-2">
      {running.length > 0 && (
        <span className="flex items-center gap-1.5 text-xs text-fg-2" title={running.map(([n]) => n).join(', ')}>
          <span className="loading loading-spinner loading-xs text-primary" />
          analysing {running.map(([n]) => n).join(', ')}
        </span>
      )}
      {changedAt && (
        <button
          type="button"
          className="btn btn-warning btn-soft btn-xs"
          title="Files changed since the analysis: analyse again"
          onClick={() => void reloadLoaded()}
        >
          <Icon name="refresh" className="size-3.5" />
          Repository changed · reanalyse
        </button>
      )}
    </div>
  )
}

export function Header({ views, active }: { views: ViewDef[]; active: string }) {
  const env = envelope()
  const { status } = useServerStatus()
  const [theme, setTheme] = useTheme()
  const repo = env.repository || status?.repository || ''
  return (
    <header className="flex h-11 shrink-0 items-center gap-3 border-b border-line bg-base-100 px-3 print:hidden">
      <a href={routeHref(views[0]?.id ?? '')} className="flex shrink-0 items-center gap-2 text-primary" title="GitView">
        <Logo className="size-5" />
        <span className="font-semibold text-fg">GitView</span>
      </a>
      {repo && (
        <>
          <span className="text-fg-3">/</span>
          <span className="max-w-56 truncate font-medium text-fg-2" title={status?.root || repo}>
            {repo}
          </span>
          {status?.branch && (
            <span className="badge badge-ghost badge-sm hidden max-w-40 truncate font-mono 2xl:inline-flex" title={status.branch}>
              {status.branch}
            </span>
          )}
        </>
      )}
      <nav role="tablist" aria-label="Views" className="tabs tabs-box tabs-sm ml-2 shrink-0 flex-nowrap bg-base-200 p-0.5">
        {views.map((v) => (
          <ViewTab key={v.id} view={v} active={v.id === active} />
        ))}
      </nav>
      <div id="gv-header-slot" className="flex min-w-0 flex-1 items-center justify-end gap-2 overflow-hidden" />
      <LiveState />
      <button
        type="button"
        className="btn btn-ghost btn-sm btn-square"
        title={theme === 'dark' ? 'Light theme' : 'Dark theme'}
        aria-label="Toggle theme"
        onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
      >
        <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
      </button>
    </header>
  )
}
