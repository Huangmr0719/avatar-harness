import { writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { GLTFExporter } from 'three/addons/exporters/GLTFExporter.js'
import { createChengAvatar } from '../src/characters/cheng.ts'

// GLTFExporter only needs Blob -> ArrayBuffer here; no DOM or canvas is needed.
export async function exportChengGLB() {
  const previous = globalThis.FileReader
  globalThis.FileReader = class {
    readAsArrayBuffer(blob) {
      blob.arrayBuffer().then(result => { this.result = result; this.onloadend?.() }).catch(error => this.onerror?.(error))
    }
  }
  try {
    return await new GLTFExporter().parseAsync(createChengAvatar(), { binary: true })
  } finally {
    if (previous) globalThis.FileReader = previous
    else delete globalThis.FileReader
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const output = new URL('../public/models/cheng.glb', import.meta.url)
  const bytes = await exportChengGLB()
  await writeFile(output, Buffer.from(bytes))
  console.log(`Generated ${fileURLToPath(output)} (${bytes.byteLength.toLocaleString()} bytes); no downloads, textures or Blender.`)
}
