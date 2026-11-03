import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
const target = new URL('../public/vendor/ort/', import.meta.url)
mkdirSync(target, { recursive: true })
for (const file of ['ort-wasm-simd-threaded.mjs', 'ort-wasm-simd-threaded.wasm']) {
  copyFileSync(new URL(`../node_modules/onnxruntime-web/dist/${file}`, import.meta.url), new URL(file, target))
}

// Renderer 1.4.0 creates an opaque WebGL context. Enable alpha at creation;
// setClearAlpha later cannot make an opaque canvas compositable over video.
for (const [name, opaque, transparent] of [
  ['esm.js', 'this.renderer = new WebGLRenderer({\n        antialias: false,', 'this.renderer = new WebGLRenderer({\n        alpha: true,\n        antialias: false,'],
  ['esm.min.js', 'antialias:!1,precision:"highp",canvas:this.canvas', 'alpha:!0,antialias:!1,precision:"highp",canvas:this.canvas'],
]) {
  const lam = new URL(`../node_modules/@myned-ai/gsplat-flame-avatar-renderer/dist/gsplat-flame-avatar-renderer.${name}`, import.meta.url)
  const source = readFileSync(lam, 'utf8').replace(/\r\n/g, '\n')
  if (source.includes(opaque) && !source.includes(transparent)) writeFileSync(lam, source.replace(opaque, transparent))
  else if (!source.includes(transparent)) throw new Error('LAM renderer context creation changed; review transparency support')
}
