import { Component, Suspense, useEffect, type ReactNode } from 'react'
import { Header } from './components/Header'
import { StatusBar } from './components/StatusBar'
import { Toaster } from './components/Toast'
import { EmptyState, ErrorState, Loading } from './components/States'
import { envelope } from './lib/envelope'
import { offered, startLive } from './lib/data'
import { navigate, useRoute } from './lib/router'
import { VIEWS } from './views'

function defaultView(): string | undefined {
  const env = envelope()
  if (env.view && VIEWS.some((v) => v.id === env.view && offered(v.section))) return env.view
  return VIEWS.find((v) => offered(v.section))?.id
}

/** ViewBoundary keeps a failing view from taking the whole page down. */
class ViewBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error) {
      return <ErrorState title="This view failed" message={String(this.state.error.stack || this.state.error.message)} />
    }
    return this.props.children
  }
}

export default function App() {
  const route = useRoute()
  const env = envelope()

  useEffect(() => {
    startLive()
    document.title = env.title || 'GitView'
  }, [env.title])

  const view = VIEWS.find((v) => v.id === route.view && offered(v.section))
  useEffect(() => {
    if (!view) {
      const d = defaultView()
      if (d) navigate(d, undefined, { replace: true })
    }
  }, [view])

  const View = view?.component
  return (
    <div className="flex h-full flex-col print:block print:h-auto">
      <Header views={VIEWS} active={view?.id ?? ''} />
      <div className="relative min-h-0 flex-1 print:min-h-fit">
        {View ? (
          <ViewBoundary key={view.id}>
            <Suspense fallback={<Loading />}>
              <View params={route.params} />
            </Suspense>
          </ViewBoundary>
        ) : (
          <EmptyState icon="info" title="Nothing to show">
            This page carries no data GitView can display.
          </EmptyState>
        )}
      </div>
      <StatusBar />
      <Toaster />
    </div>
  )
}
