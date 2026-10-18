// The views of the app, in tab order. Each one reads one section of the
// page's data; `available` refines, once the data is there, whether the tab
// is shown (an offline report exported without Code Flow has no Flow tab).

import { lazy, type ComponentType } from 'react'
import type { IconName } from './components/Icon'
import type { SectionName } from './lib/envelope'
import type { ReportSection } from './lib/types'

// Each view is its own module, loaded when first opened (the production
// build still inlines them all in the one page). This module is data, not a
// component file, so fast refresh does not apply to it.
/* oxlint-disable react/only-export-components */
const ArchitectureView = lazy(() => import('./features/architecture'))
const HttpView = lazy(() => import('./features/http'))
const SecurityView = lazy(() => import('./features/security'))
const ChangesView = lazy(() => import('./features/changes'))
const TestsView = lazy(() => import('./features/tests'))
const FlowView = lazy(() => import('./features/flow'))
const InspectView = lazy(() => import('./features/inspect'))
const StudyView = lazy(() => import('./features/study'))
const RuntimeView = lazy(() => import('./features/runtime'))

export interface ViewProps {
  /** The route's parameters (#/<view>?key=value). */
  params: URLSearchParams
}

export interface ViewDef {
  id: string
  label: string
  icon: IconName
  section: SectionName
  component: ComponentType<ViewProps>
  available?: (data: unknown) => boolean
}

const report = (pick: (r: ReportSection) => unknown) => (d: unknown) => !!pick(d as ReportSection)

export const VIEWS: ViewDef[] = [
  { id: 'architecture', label: 'Architecture', icon: 'graph', section: 'graph', component: ArchitectureView },
  { id: 'http', label: 'HTTP', icon: 'globe', section: 'graph', component: HttpView },
  { id: 'security', label: 'Security', icon: 'shield', section: 'security', component: SecurityView },
  { id: 'changes', label: 'Changes', icon: 'diff', section: 'report', component: ChangesView },
  { id: 'tests', label: 'Tests', icon: 'beaker', section: 'report', component: TestsView, available: report((r) => r.tests) },
  { id: 'flow', label: 'Code Flow', icon: 'flow', section: 'report', component: FlowView, available: report((r) => r.flow) },
  { id: 'inspect', label: 'Inspector', icon: 'microscope', section: 'report', component: InspectView, available: report((r) => r.inspect) },
  { id: 'study', label: 'Study', icon: 'book', section: 'report', component: StudyView, available: report((r) => r.study) },
  { id: 'runtime', label: 'Runtime', icon: 'gauge', section: 'runtime', component: RuntimeView },
]
