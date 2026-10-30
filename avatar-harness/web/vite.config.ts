import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readFileSync } from 'node:fs'

export default defineConfig({
  plugins: [vue(), {
    name: 'local-onnx-runtime',
    configureServer(server) {
      // ORT imports its local Emscripten module dynamically. Serve it before
      // Vite's public-file import guard; production uses the Go static server.
      server.middlewares.use((req, res, next) => {
        const name = req.url?.split('?')[0]
        const file = name?.slice('/vendor/ort/'.length)
        if (!name?.startsWith('/vendor/ort/') || !['ort-wasm-simd-threaded.mjs'].includes(file!)) return next()
        res.setHeader('Content-Type', 'text/javascript')
        res.end(readFileSync(new URL(`./public/vendor/ort/${file}`, import.meta.url)))
      })
    },
  }],
  server: { port: 5173, proxy: { '/api': 'http://127.0.0.1:8765', '/v1': 'http://127.0.0.1:8765' } },
})
