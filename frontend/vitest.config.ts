import { defineConfig } from 'vitest/config'

// Unit tests of the pure parts of the web UI (layout documents, geometry,
// edge routing, scene editing): `npm test`. The browser checks are manual
// (see README) — these need no DOM.
export default defineConfig({
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
  },
})
