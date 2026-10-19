// Inspector (right pane): what is selected, in tabs.
//   element   Overview · Connections · Source · Metadata
//   relation  the relations a drawn line stands for, with their evidence
//   nothing   the repository in numbers, and how it was analysed

import type { ReactNode } from 'react'
import { Icon } from '../../components/Icon'
import { PaneHeader } from '../../components/Panes'
import type { Explorer, Snapshot } from './controller'
import { EdgeInspector } from './inspector/Edge'
import { ItemInspector } from './inspector/Item'
import { MultiInspector } from './inspector/Multi'
import { SummaryInspector } from './inspector/Summary'
import type { Model } from './model'
import type { SecuritySectionPeek } from './types'

export interface InspectorProps {
  D: Model
  ctrl: Explorer
  snap: Snapshot
  security: SecuritySectionPeek | null
  securityOffered: boolean
  onHide: () => void
}

export function Inspector(props: InspectorProps) {
  const { snap, ctrl, onHide } = props
  const sel = snap.S.sel
  let body: ReactNode
  if (sel?.kind === 'item' && snap.S.multi.size > 1) body = <MultiInspector {...props} />
  else if (sel?.kind === 'item') body = <ItemInspector key={'i' + sel.i} {...props} i={sel.i} />
  else if (sel?.kind === 'edge') {
    const e = ctrl.edge(sel.key)
    body = e ? <EdgeInspector key={'e' + sel.key} {...props} e={e} /> : <SummaryInspector {...props} />
  } else body = <SummaryInspector {...props} />
  return (
    <>
      <PaneHeader title="Inspector">
        <button
          type="button"
          className="btn btn-ghost btn-xs btn-square"
          title="Hide the inspector (i)"
          aria-label="Hide the inspector"
          onClick={onHide}
        >
          <Icon name="chevronRight" className="size-3.5" />
        </button>
      </PaneHeader>
      {body}
    </>
  )
}
