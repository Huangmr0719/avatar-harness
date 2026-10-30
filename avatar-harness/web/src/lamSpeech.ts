import type { MotionFrame } from './types'
import { speechEnergyCurve } from './speechEnergy.ts'
import { ARKIT_NAMES } from './arkit.ts'

// A rolling second of real PCM supplies the model's fixed input. Only the newly
// received suffix is emitted, so context is never replayed as new motion.
export class LamSpeech {
  private worker?: Worker
  private remoteRequests = new Set<AbortController>()
  private loadController = new AbortController()
  private sequence = 0
  private requests = new Map<number, { resolve: (data: any) => void; reject: (error: Error) => void }>()
  private context = new Float32Array(16000)
  private names: string[] = []
  private generation = 0
  ready: Promise<void>
  lastInferenceMS = 0
  backend = ''
  private remoteURL?: string
  constructor(remoteURL?: string) {
    this.remoteURL = remoteURL
    if (remoteURL) {
      this.ready = this.remote({}, true).then(data => {
        if (data.input_samples !== 34133 || data.sample_rate !== 16000 || !Array.isArray(data.names) || data.names.join(',') !== ARKIT_NAMES.join(',')) throw new Error('远程 LAM 驱动格式不匹配')
        this.names = data.names; this.context = new Float32Array(data.input_samples); this.backend = data.backend
      })
      return
    }
    this.worker = new Worker(new URL('./lam.worker.ts', import.meta.url), { type: 'module' })
    this.worker.onmessage = ({ data }) => {
      const request = this.requests.get(data.id)
      this.requests.delete(data.id)
      if (data.error) request?.reject(new Error(data.error))
      else request?.resolve(data)
    }
    this.worker.onerror = event => { this.fail(new Error(event.message)) }
    this.ready = this.request({ type: 'load' }).then(data => { this.names = data.names; this.backend = data.backend })
  }
  private request(data: Record<string, unknown>, transfer: Transferable[] = []): Promise<any> {
    if (this.remoteURL) return this.remote(data)
    const id = ++this.sequence
    return new Promise((resolve, reject) => { this.requests.set(id, { resolve, reject }); this.worker!.postMessage({ ...data, id }, transfer) })
  }
  private async remote(data: Record<string, unknown>, load = false): Promise<any> {
    const controller = load ? this.loadController : new AbortController()
    if (!load) this.remoteRequests.add(controller)
    const timer = setTimeout(() => controller.abort(), 10000)
    try {
      let body: string | undefined
      if (!load) {
        const bytes = new Uint8Array((data.audio as Float32Array).buffer)
        let encoded = ''
        for (let offset = 0; offset < bytes.length; offset += 8192) encoded += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
        body = JSON.stringify({ audio_base64: btoa(encoded) })
      }
      const response = await fetch(this.remoteURL!, { method: load ? 'GET' : 'POST', headers: body ? { 'Content-Type': 'application/json' } : undefined, body, signal: controller.signal })
      const result = await response.json()
      if (!response.ok) throw new Error(result.detail || `远程语音驱动 HTTP ${response.status}`)
      if (load) return result
      if (!Array.isArray(result.weights) || result.weights.length !== 64 * 52 || result.weights.some((v: unknown) => typeof v !== 'number' || !Number.isFinite(v))) throw new Error('远程模型返回了无效动作')
      return { ...result, weights: Float32Array.from(result.weights) }
    } finally { clearTimeout(timer); this.remoteRequests.delete(controller) }
  }
  async prepare(buffer: AudioBuffer, pts: number): Promise<MotionFrame[]> {
    const generation = this.generation
    await this.ready
    if (generation !== this.generation) return []
    const mono = new Float32Array(buffer.length)
    for (let c = 0; c < buffer.numberOfChannels; c++) {
      const input = buffer.getChannelData(c)
      for (let i = 0; i < mono.length; i++) mono[i] += input[i] / buffer.numberOfChannels
    }
    const count = Math.round(buffer.duration * 16000), samples = new Float32Array(count)
    for (let i = 0; i < count; i++) {
      const position = i * buffer.sampleRate / 16000, a = Math.floor(position), t = position - a
      samples[i] = (mono[a] ?? 0) * (1 - t) + (mono[a + 1] ?? 0) * t
    }
    const frames: MotionFrame[] = []
    // Handle WAV providers with long buffers as well as short realtime PCM.
    for (let offset = 0; offset < count; offset += 8000) {
      const part = samples.subarray(offset, Math.min(count, offset + 8000))
      this.context.copyWithin(0, part.length)
      this.context.set(part, this.context.length - part.length)
      const audio = this.context.slice()
      let result
      try { result = await this.request({ type: 'infer', audio }, [audio.buffer]) }
      catch (error) { if (generation !== this.generation) return []; throw error }
      if (generation !== this.generation) return []
      this.lastInferenceMS = result.elapsed_ms
      const total = result.weights.length / 52, duration = part.length / 16
      const n = Math.max(1, Math.round(duration * total / (this.context.length / 16)))
      const energy = speechEnergyCurve(part, n, this.context)
      for (let i = 0; i < n; i++) {
        const index = Math.max(0, total - n + i)
        const blendshapes: Record<string, number> = {}
        this.names.forEach((name, k) => { blendshapes[name] = Math.max(0, Math.min(1, result.weights[index * 52 + k])) })
        frames.push({ pts_ms: pts + offset / 16 + i * duration / n, blendshapes, speech_energy: energy[i] })
      }
    }
    return frames
  }
  reset() { this.generation++; this.context.fill(0); this.remoteRequests.forEach(controller => controller.abort()); this.remoteRequests.clear() }
  private fail(error: Error) { for (const request of this.requests.values()) request.reject(error); this.requests.clear() }
  dispose() { this.reset(); this.loadController.abort(); this.fail(new Error('语音驱动已关闭')); this.worker?.terminate() }
}

export type AudioPacket = { audio_base64: string; format?: string; sample_rate?: number; channels?: number; pts_ms?: number }

// Target 400ms and cap at 500ms (one inference window), even if the cloud
// bursts a much larger packet. Flush a short tail after 70ms.
export class SpeechBatcher {
  private packets: AudioPacket[] = []
  private duration = 0
  private timer?: ReturnType<typeof setTimeout>
  private output: (packet: AudioPacket) => void
  constructor(output: (packet: AudioPacket) => void) { this.output = output }
  add(packet: AudioPacket) {
    const first = this.packets[0]
    if (packet.format !== 'pcm_s16le') { this.flush(); this.output(packet); return }
    if (first && (first.sample_rate !== packet.sample_rate || first.channels !== packet.channels || Math.abs((first.pts_ms ?? 0) + this.duration - (packet.pts_ms ?? 0)) > 2)) this.flush()
    clearTimeout(this.timer)
    const bytes = atob(packet.audio_base64), rate = packet.sample_rate ?? 24000, alignment = 2 * (packet.channels ?? 1)
    if (!Number.isFinite(rate) || rate < 2 || !Number.isInteger(alignment) || alignment < 2 || bytes.length % alignment !== 0) {
      this.flush(); this.output(packet); return
    }
    let offset = 0
    while (offset < bytes.length) {
      const capacity = Math.floor((500 - this.duration) * rate / 1000 + 1e-6) * alignment
      if (capacity < alignment) { this.flush(); continue }
      const end = Math.min(bytes.length, offset + capacity)
      this.packets.push({ ...packet, pts_ms: (packet.pts_ms ?? 0) + offset / alignment / rate * 1000, audio_base64: btoa(bytes.slice(offset, end)) })
      this.duration += (end - offset) / alignment / rate * 1000
      offset = end
      if (this.duration >= 400 - 1e-6) this.flush()
    }
    if (this.packets.length) this.timer = setTimeout(() => this.flush(), 70)
  }
  flush() {
    clearTimeout(this.timer)
    if (!this.packets.length) return
    const first = this.packets[0], data = this.packets.map(p => atob(p.audio_base64)).join('')
    this.packets = []; this.duration = 0
    this.output({ ...first, audio_base64: btoa(data) })
  }
  reset() { clearTimeout(this.timer); this.packets = []; this.duration = 0 }
}
