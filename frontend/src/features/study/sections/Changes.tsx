import { useStudy } from '../context'
import {
  BasisBadge,
  Card,
  Empty,
  EvidenceList,
  LayerBadge,
  LocLink,
  NodeName,
  Note,
  PathText,
  SectionTitle,
  Tag,
  TrackedLink,
} from '../ui'

export function Changes() {
  const { model, links } = useStudy()
  const groups = model.changes ?? []
  return (
    <>
      <Note className="mb-3">
        Files of the working-tree diff grouped by the layer their changed functions belong to (Go), or by name and
        extension (other files), with the calls that connect them.
      </Note>
      {!groups.length && <Empty>No changed files in this report.</Empty>}
      {groups.map((g) => (
        <section key={g.layer}>
          <SectionTitle className="flex items-center gap-2">
            <LayerBadge layer={g.layer} className="badge-md" />
            <span className="text-xs font-normal text-fg-3">
              {g.files.length === 1 ? '1 file' : g.files.length + ' files'}
            </span>
          </SectionTitle>
          <div className="space-y-2">
            {g.files.map((f) => {
              const href = links.diff(f.path, 0)
              return (
                <Card key={f.path} changed>
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    {href ? (
                      <TrackedLink
                        href={href}
                        area="files"
                        id={f.path}
                        className="link link-hover font-mono text-[13px] font-semibold break-words text-accent-text"
                      >
                        <PathText text={f.path} />
                      </TrackedLink>
                    ) : (
                      <span className="font-mono text-[13px] font-semibold break-words">
                        <PathText text={f.path} />
                      </span>
                    )}
                    <span className="font-mono text-xs text-success">+{f.added}</span>
                    <span className="font-mono text-xs text-error">−{f.deleted}</span>
                    <BasisBadge basis={f.basis} />
                  </div>
                  <EvidenceList items={f.evidence} className="mt-2" />
                  {!!f.symbols?.length && (
                    <div className="mt-2 flex flex-wrap items-center gap-1 text-xs">
                      <span className="mr-1 text-fg-3">changed functions:</span>
                      {f.symbols.map((s) => (
                        <span key={s.id} className="rounded-full border border-line bg-surface-2 px-2 py-px text-xs">
                          <NodeName id={s.id} name={s.name} />
                        </span>
                      ))}
                    </div>
                  )}
                  {!!f.related?.length && (
                    <ul className="mt-2 space-y-1 text-xs">
                      {f.related.map((r, i) => (
                        <li key={i} className="flex flex-wrap items-center gap-1.5">
                          <BasisBadge basis="STATIC" />
                          <span className="text-fg-3">{r.dir}</span>
                          <NodeName id={r.to} name={r.toName} />
                          <LayerBadge layer={r.layer} />
                          {r.changed && <Tag tone="primary">changed</Tag>}
                          <LocLink file={r.evidence.file} line={r.evidence.line} href={links.href(r.evidence.ref)} />
                        </li>
                      ))}
                    </ul>
                  )}
                </Card>
              )
            })}
          </div>
        </section>
      ))}
    </>
  )
}
