import { envelope, isLive } from '../lib/envelope'
import { dateTime } from '../lib/format'

export function StatusBar() {
  const env = envelope()
  return (
    <footer className="flex h-6 shrink-0 items-center gap-4 border-t border-line bg-base-100 px-3 text-[11px] text-fg-3 print:hidden">
      <div id="gv-status-slot" className="flex min-w-0 flex-1 items-center gap-4 truncate" />
      {isLive() ? (
        <span title="Served by gitview serve on this machine only (127.0.0.1)">
          <span className="status status-success mr-1.5" />
          live · {location.host}
        </span>
      ) : (
        <span title="The page contains everything it shows: it makes no network request.">offline</span>
      )}
      {!isLive() && env.generated && <span>generated {dateTime(env.generated)}</span>}
    </footer>
  )
}
