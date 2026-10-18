import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { viteSingleFile } from 'vite-plugin-singlefile'

// The build is one self-contained index.html (scripts and styles inline):
// the Go side embeds it (frontend.go), pins its script by hash in the
// Content-Security-Policy and adds the data, so the same page works offline
// from file:// and served by `gitview serve`.
//
// In development `npm run dev` proxies /api to a running
// `gitview serve --open=false --port 7777` (GITVIEW_API overrides the
// address); open the printed URL's ?t=<token> on the Vite port.
const api = process.env.GITVIEW_API ?? 'http://127.0.0.1:7777'

export default defineConfig({
  plugins: [react(), tailwindcss(), viteSingleFile({ removeViteModuleLoader: true })],
  build: {
    target: 'es2022',
    modulePreload: { polyfill: false },
    chunkSizeWarningLimit: 4096,
    reportCompressedSize: false,
  },
  server: {
    proxy: {
      '/api': {
        target: api,
        changeOrigin: true,
        // The Go server only accepts its own origin; requests coming
        // through the proxy are same-machine development requests.
        configure: (proxy) => proxy.on('proxyReq', (req) => req.removeHeader('origin')),
      },
    },
  },
})
