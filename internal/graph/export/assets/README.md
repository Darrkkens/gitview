# Page assets

The Repository Graph HTML page is assembled by `../html.go` from:

- `../page/*.html`: html/template partials (header, sidebar, canvas, inspector, overlays, status bar).
- `css/`: the design system (`tokens.css`) and one stylesheet per component, concatenated in the order of `cssFiles`.
- `js/`: one module per component, concatenated in the order of `jsFiles`. Each one adds itself to the `GV` namespace and becomes its own inline script, pinned by its SHA-256 in the page's Content-Security-Policy. A test checks that every file is listed.
- `vendor/`: third-party code, embedded unmodified.

## Vendored library

Embedded so the page works offline (no CDN). The page's CSP (`default-src 'none'`, no `connect-src`, no workers) blocks every network request anyway.

| File | Package | License | SHA-256 of the npm file |
|---|---|---|---|
| `vendor/elk-0.11.0.bundled.js` | `elkjs@0.11.0` `lib/elk.bundled.js` | EPL-2.0 (`vendor/LICENSE.elk.md`) | `cbf61b0182e9085d36dcd5b392f57cc816273169ac40bde80b52b808444c5cf8` |

ELK runs in the page's thread through the bundle's built-in fake worker: the page never passes `workerUrl`, so no Worker is created. `TestPagePartsAreListed` checks the hash.
