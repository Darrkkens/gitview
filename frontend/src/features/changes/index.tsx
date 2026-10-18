import { SectionGate } from '../../components/States'
import type { ViewProps } from '../../views'
import { Changes } from './Changes'
import type { ReportSection } from './types'
import './diff.css'

export default function ChangesView({ params }: ViewProps) {
  return (
    <SectionGate<ReportSection> section="report" loadingLabel="Reading the working tree…">
      {(r) => <Changes key={r.generated + ':' + r.files.length} report={r} params={params} />}
    </SectionGate>
  )
}
