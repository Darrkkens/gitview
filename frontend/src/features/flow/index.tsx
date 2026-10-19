import { EmptyState } from '../../components/States'
import { SectionGate } from '../../components/States'
import { Icon } from '../../components/Icon'
import type { ViewProps } from '../../views'
import { Flow } from './Flow'
import type { FlowReport } from './types'
import './flow.css'

export default function FlowView({ params }: ViewProps) {
  return (
    <SectionGate<FlowReport> section="report" loadingLabel="Analysing the Go code…">
      {(r) => {
        const f = r.flow
        if (!f)
          return (
            <EmptyState icon="flow" title="Code Flow is not in this report">
              Export the report with the “Code Flow” option to embed the call index of the module.
            </EmptyState>
          )
        if (f.error || !f.nodes)
          return (
            <div className="flex h-full items-center justify-center p-6">
              <div role="alert" className="alert max-w-2xl alert-soft alert-warning items-start">
                <Icon name="warning" className="size-5" />
                <div className="min-w-0">
                  <h3 className="font-semibold">Code Flow is not available in this report</h3>
                  <p className="text-sm break-words whitespace-pre-wrap">{f.error || 'no Go code was analysed'}</p>
                </div>
              </div>
            </div>
          )
        return <Flow key={r.generated} report={r} flow={f} params={params} />
      }}
    </SectionGate>
  )
}
