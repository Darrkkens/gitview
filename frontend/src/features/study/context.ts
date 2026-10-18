// The Study context: the model, the link resolver and the viewer's progress.

import { createContext, useContext } from 'react'
import type { Links } from './links'
import type { Progress } from './progress'
import type { StudyModel } from './types'

export interface StudyCtx {
  model: StudyModel
  links: Links
  progress: Progress
}

export const StudyContext = createContext<StudyCtx | null>(null)

export function useStudy(): StudyCtx {
  const c = useContext(StudyContext)
  if (!c) throw new Error('Study context missing')
  return c
}
