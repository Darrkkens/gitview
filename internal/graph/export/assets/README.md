# Vendored browser libraries

Embedded into the Repository Graph HTML report so it works offline (no CDN).
Both files are the unmodified npm builds except for the trailing
`//# sourceMappingURL=` comment, which was removed so browsers never try to
load a source map.

| File | Package | License | SHA-256 of the npm file |
|---|---|---|---|
| `sigma-3.0.2.min.js` | `sigma@3.0.2` `dist/sigma.min.js` | MIT (`LICENSE.sigma.txt`) | `be6f790da9c1856765b1c430de3b1aac50433223361870edb3097a9868218a88` |
| `graphology-0.26.0.umd.min.js` | `graphology@0.26.0` `dist/graphology.umd.min.js` | MIT (`LICENSE.graphology.txt`) | `dc337efa23903f61e064c8e7e7f93a429e6855dccfc2458802b4ed30c621c087` |

Sigma renders with WebGL. Its image loader contains a `fetch` call that the
report never uses; the report's Content-Security-Policy (`default-src 'none'`)
blocks every network request anyway.
