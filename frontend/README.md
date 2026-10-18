# GitView web UI

The web UI of GitView: **Vite + React + TypeScript**, styled with **Tailwind CSS v4** and **daisyUI 5**.
`npm run build` produces one self-contained file, `dist/index.html` (every script and style inline), which is
committed and embedded by the Go binary (`frontend.go`). `internal/webui` completes it into a page: it adds the
data as an inert JSON block and a Content-Security-Policy that pins the inline script by its SHA-256.

The same page serves every surface:

| Surface | Command | Data |
|---|---|---|
| Local app | `gitview serve` | fetched from `/api/section/<name>` (token in the URL) |
| Repository Graph | `gitview graph export` | `graph` section inline, sources compressed |
| Security Journey | `gitview audit export` | `graph` + `security` inline |
| Session report | TUI `x`, `gitview study`, `gitview inspect report` | `report` inline |
| Runtime report | `gitview runtime show -o`, `compare -o` | `runtime` inline |

Offline pages make no network request (`default-src 'none'`, no `connect-src`); served pages may only call the
server that served them.

## Development

```sh
make dev                 # gitview serve on :7777 + Vite on :5173 → http://localhost:5173/?t=dev
make dev DIR=~/dev/api   # another repository
```

Or by hand, any repository and ports:

```sh
GITVIEW_SERVE_TOKEN=dev go run ./cmd/gitview serve --open=false --port 7777 ~/dev/api
GITVIEW_API=http://127.0.0.1:7777 npx vite --port 5173      # open http://localhost:5173/?t=dev
```

Offline pages, without a build: extract the data of any exported page and open it with `?sample=`:

```sh
gitview graph export -o /tmp/g.html
npm run sample -- /tmp/g.html graph      # → .samples/graph.json (git-ignored)
# http://localhost:5173/?sample=graph#/architecture
```

Before committing: `make web` (rebuilds `dist/index.html`; CI checks it is up to date), `npm run typecheck`,
`npm run lint`, `go test ./...`.

## Layout

```
src/
  main.tsx, App.tsx       shell: header with the view tabs, the active view, status bar
  views.tsx               the views (tab order, section, availability), lazily loaded
  styles/global.css       Tailwind, the daisyUI themes gitview-dark / gitview-light, domain tokens
  lib/                    envelope (page data), data (sections, serve mode), api, router (#/view?k=v),
                          sources (embedded Go files), go (tokenizer), theme, tokens (CSS vars for canvas), format
  components/             shared UI: Header, StatusBar, Slots (HeaderSlot/StatusSlot), Code (CodeView,
                          CodeInline, ChromaSpans), Panes, States (Loading/Error/Empty/SectionGate), Toast, CopyButton, Icon
  features/<view>/        one folder per view; index.tsx is the view
```

## Conventions

- **Code is data.** Repository content is only rendered as text (React escapes it). `dangerouslySetInnerHTML`,
  `innerHTML`, `eval` and friends are forbidden; `frontend_test.go` fails the build if they appear.
- **daisyUI first**: `btn`, `badge`, `tabs`, `table`, `menu`, `card`, `collapse`, `alert`, `tooltip`, `modal`,
  `dropdown`, `input`, `select`, `toggle`, `kbd`, `stat`, `steps`, `timeline`, `join`… with Tailwind utilities for
  layout. GitView is dense: the root font is 14px; prefer `text-xs`/`text-sm`, `btn-xs`/`btn-sm`, `badge-sm`,
  `table-xs`.
- **Domain colours** come from tokens exposed as utilities: `text-fg`, `text-fg-2`, `text-fg-3`, `border-line`,
  `bg-surface-2`, `bg-code`; element kinds `k-*` (`k-package`, `k-func`…), layers `l-*`, confidence `c-*`, HTTP
  methods `m-*`, security states `s-*`, severities `sev-*`, diff `diff-add`/`diff-del`. Canvas code reads the same
  tokens with `token('k-func')` and redraws when `useTheme()` changes.
- **State in the URL** when it is worth sharing: `useRoute()`, `navigate(view, params)`, `setParams(params)`.
  Cross-view links: `#/architecture?node=<id>`, `#/http?route=<id>`, `#/security?finding=<id>`,
  `#/changes?file=<path>&line=<n>`, `#/flow?node=<id>`, `#/inspect?fn=<id>`, `#/study?step=<n>`,
  `#/runtime?capture=<id>`.
- **Per-viewer conveniences only** in `localStorage` (theme, pane widths), always in `try/catch`.
- UI text is in English, like the exported reports.
