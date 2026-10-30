import * as ort from 'onnxruntime-web/wasm'
import { ARKIT_NAMES } from './arkit'

let session: ort.InferenceSession
let names: string[]
let queue = Promise.resolve()

self.onmessage = ({ data }) => {
  queue = queue.then(async () => {
    try {
      if (data.type === 'load') {
        // The community config lists Apple's order, but this model retains the
        // alphabetical LAM/Wav2ARKit training order (jawOpen is index 24).
        names = [...ARKIT_NAMES]
        ort.env.wasm.wasmPaths = '/vendor/ort/'
        ort.env.wasm.numThreads = 1
        ort.env.logLevel = 'error'
        const backend = 'wasm'
        await load(['wasm'])
        self.postMessage({ id: data.id, names, backend })
      } else {
        const start = performance.now()
        const weights = await infer(data.audio)
        self.postMessage({ id: data.id, weights, elapsed_ms: performance.now() - start }, { transfer: [weights.buffer as ArrayBuffer] })
      }
    } catch (error) { self.postMessage({ id: data.id, error: String(error) }) }
  })
}

async function load(executionProviders: string[]) {
  session = await ort.InferenceSession.create('/models/lam-a2e/lam_ios.onnx', {
    executionProviders,
    externalData: [{ path: 'lam_ios.onnx.data', data: '/models/lam-a2e/lam_ios.onnx.data' }],
  })
  await infer(new Float32Array(16000))
}

async function infer(audio: Float32Array): Promise<Float32Array> {
  const identity = new Float32Array(12); identity[0] = 1
  const result = await session.run({ audio: new ort.Tensor('float32', audio, [1, 16000]), identity: new ort.Tensor('float32', identity, [1, 12]) })
  const output = result.blendshapes
  if (!output || output.dims.at(-1) !== 52) throw new Error('音频表情模型输出格式不匹配')
  const values = Float32Array.from(output.data as Float32Array)
  for (const tensor of Object.values(result)) tensor.dispose()
  return values
}
