import { EmptyState } from '../../components/States'
import { SectionGate } from '../../components/States'
import { isLive } from '../../lib/envelope'
import type { ViewProps } from '../../views'
import { Tests } from './Tests'
import type { ReportSection } from './types'

export default function TestsView({ params }: ViewProps) {
  return (
    <SectionGate<ReportSection> section="report">
      {(r) =>
        r.tests?.ran ? (
          <Tests tests={r.tests} params={params} />
        ) : (
          <EmptyState icon="beaker" title="Tests were not run in this session">
            {isLive()
              ? 'gitview serve never runs tests: they execute code from the repository. Run them in the GitView TUI (t), then export the report (x) to include the results.'
              : 'Run the tests in the GitView TUI (t) before exporting the report (x) and their results, command and log appear here. Nothing is invented for a run that did not happen.'}
          </EmptyState>
        )
      }
    </SectionGate>
  )
}
