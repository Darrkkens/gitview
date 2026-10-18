import { BASIS_HELP } from '../fmt'
import type { Analysis } from '../types'
import { BasisBadge, Card, H2, Muted } from '../ui'

export function Limitations({ a }: { a: Analysis }) {
  return (
    <>
      <H2>What this capture could not measure</H2>
      {a.limitations?.length ? (
        <Card>
          <ul className="list-disc space-y-1 pl-5 text-xs leading-relaxed text-fg">
            {a.limitations.map((x, i) => (
              <li key={i}>{x}</li>
            ))}
          </ul>
        </Card>
      ) : (
        <Muted>No limitation was recorded.</Muted>
      )}
      {!!a.notes?.length && (
        <>
          <H2>Notes</H2>
          <ul className="list-disc space-y-1 pl-5 text-xs leading-relaxed text-fg-2">
            {a.notes.map((x, i) => (
              <li key={i}>{x}</li>
            ))}
          </ul>
        </>
      )}
      {!!a.stale?.length && (
        <>
          <H2>Files changed after the process started</H2>
          <Muted className="mb-1">Lines in these files may not match the code that ran.</Muted>
          <ul className="list-disc space-y-0.5 pl-5 font-mono text-xs">
            {a.stale.map((x, i) => (
              <li key={i}>{x}</li>
            ))}
          </ul>
        </>
      )}
      <H2>How to read the values</H2>
      <Card>
        <ul className="space-y-1.5 text-xs">
          {Object.entries(BASIS_HELP).map(([b, help]) => (
            <li key={b} className="flex items-center gap-2">
              <BasisBadge basis={b} className="w-20 justify-center" />
              <span className="text-fg-2">{help}</span>
            </li>
          ))}
        </ul>
        <Muted className="mt-2">
          Process metrics cover everything the process did during the capture; per-request numbers are sampled or
          estimated and say so. A value that could not be collected is shown as unavailable, never as zero.
        </Muted>
      </Card>
    </>
  )
}
