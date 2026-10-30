import { copyFileSync, mkdirSync } from 'node:fs'
const target = new URL('../public/vendor/ort/', import.meta.url)
mkdirSync(target, { recursive: true })
for (const file of ['ort-wasm-simd-threaded.mjs', 'ort-wasm-simd-threaded.wasm']) {
  copyFileSync(new URL(`../node_modules/onnxruntime-web/dist/${file}`, import.meta.url), new URL(file, target))
}
