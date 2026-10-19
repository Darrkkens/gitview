// Temporary view used until a feature is ported: lists the section's keys.

export function Placeholder({ name, data }: { name: string; data: unknown }) {
  const keys = data && typeof data === 'object' ? Object.keys(data as object) : []
  return (
    <div className="p-6">
      <div className="card card-border bg-base-100">
        <div className="card-body">
          <h2 className="card-title">{name}</h2>
          <p className="text-sm text-fg-2">This view is being ported to React. Section keys:</p>
          <div className="flex flex-wrap gap-1">
            {keys.map((k) => (
              <span key={k} className="badge badge-soft badge-primary font-mono">
                {k}
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
