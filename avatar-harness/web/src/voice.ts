export function wav(samples: Float32Array, sampleRate: number): string {
  const bytes = new Uint8Array(44 + samples.length * 2), view = new DataView(bytes.buffer)
  const text = (offset: number, value: string) => { for (let i = 0; i < value.length; i++) bytes[offset + i] = value.charCodeAt(i) }
  text(0, 'RIFF'); view.setUint32(4, bytes.length - 8, true); text(8, 'WAVE'); text(12, 'fmt ')
  view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true)
  view.setUint32(24, sampleRate, true); view.setUint32(28, sampleRate * 2, true); view.setUint16(32, 2, true); view.setUint16(34, 16, true)
  text(36, 'data'); view.setUint32(40, samples.length * 2, true)
  for (let i = 0; i < samples.length; i++) { const value = Math.max(-1, Math.min(1, samples[i])); view.setInt16(44 + i * 2, value * (value < 0 ? 32768 : 32767), true) }
  let binary = ''; for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192))
  return btoa(binary)
}

export interface VADOptions {
  silenceMS?: number
  onsetMS?: number
  audio?: (samples: Float32Array, rate: number) => void
  discard?: () => void
}

export class PCM16k {
  private ratio: number
  private remaining: number
  private sum = 0
  constructor(rate: number) { this.ratio = rate / 16000; this.remaining = this.ratio }
  feed(samples: Float32Array): Uint8Array {
    const values: number[] = []
    for (const sample of samples) {
      let available = 1
      while (available > 1e-8) {
        const weight = Math.min(this.remaining, available)
        this.sum += Math.max(-1, Math.min(1, sample)) * weight
        available -= weight; this.remaining -= weight
        if (this.remaining < 1e-8) {
          const value = this.sum / this.ratio
          values.push(Math.round(value * (value < 0 ? 32768 : 32767)))
          this.sum = 0; this.remaining = this.ratio
        }
      }
    }
    const raw = new Uint8Array(values.length * 2), view = new DataView(raw.buffer)
    values.forEach((value, index) => view.setInt16(index * 2, value, true))
    return raw
  }
}

export class UtteranceVAD {
  private preRoll: Float32Array[] = []
  private chunks: Float32Array[] = []
  private active = false
  private started = 0
  private lastSpeech = 0
  private voicedMS = 0
  private candidateMS = 0
  private suppressed = false
  private options: VADOptions
  private onSpeech: () => void
  private onUtterance: (samples: Float32Array, rate: number, endedAt: number) => void
  constructor(onSpeech: () => void, onUtterance: (samples: Float32Array, rate: number, endedAt: number) => void, options: VADOptions = {}) { this.onSpeech = onSpeech; this.onUtterance = onUtterance; this.options = options }
  discard() { this.active = false; this.chunks = []; this.preRoll = []; this.candidateMS = 0; this.suppressed = true; this.lastSpeech = performance.now() }
  feed(samples: Float32Array, rate: number, now: number): number {
    let sum = 0; for (const sample of samples) sum += sample * sample
    const level = Math.sqrt(sum / Math.max(1, samples.length))
    const lengthMS = samples.length / rate * 1000
    this.preRoll.push(samples)
    while (this.preRoll.length > Math.ceil(300 / lengthMS)) this.preRoll.shift()
    const silenceMS = this.options.silenceMS ?? 600
    if (this.suppressed) {
      if (level > .012) this.lastSpeech = now
      else if (now - this.lastSpeech >= silenceMS) this.suppressed = false
      return level
    }
    let startedNow = false
    if (!this.active) {
      this.candidateMS = level > .018 ? this.candidateMS + lengthMS : 0
      if (this.candidateMS >= (this.options.onsetMS ?? 80)) {
        this.active = true; startedNow = true; this.started = now; this.lastSpeech = now; this.voicedMS = this.candidateMS
        this.chunks = this.options.audio ? [] : this.preRoll.slice()
        this.onSpeech()
        if (this.options.audio) for (const chunk of this.preRoll) this.options.audio(chunk, rate)
      }
    } else if (level > .012) { this.lastSpeech = now; this.voicedMS += lengthMS }
    if (this.active) {
      if (!startedNow) {
        if (this.options.audio) this.options.audio(samples, rate)
        else this.chunks.push(samples)
      }
      if (now - this.lastSpeech >= silenceMS || now - this.started >= 15000) {
        if (this.voicedMS >= 120) {
          const joined = new Float32Array(this.chunks.reduce((total, item) => total + item.length, 0)); let offset = 0
          for (const chunk of this.chunks) { joined.set(chunk, offset); offset += chunk.length }
          this.onUtterance(joined, rate, this.lastSpeech)
        } else this.options.discard?.()
        this.active = false; this.chunks = []; this.preRoll = []; this.candidateMS = 0
      }
    }
    return level
  }
}

export class VoiceInput {
  private stream?: MediaStream
  private context?: AudioContext
  private node?: AudioWorkletNode
  private source?: MediaStreamAudioSourceNode
  private generation = 0
  private vad?: UtteranceVAD
  private onLevel: (value: number) => void
  private onSpeech: () => void
  private onUtterance: (input: Record<string, unknown>, endedAt: number) => void
  constructor(onLevel: (value: number) => void, onSpeech: () => void, onUtterance: (input: Record<string, unknown>, endedAt: number) => void) { this.onLevel = onLevel; this.onSpeech = onSpeech; this.onUtterance = onUtterance }
  async start(options: { silenceMS?: number; onsetMS?: number; onError?: (error: unknown) => void; sink?: { audio: (raw: Uint8Array) => void; commit: (endedAt: number) => void; discard: () => void } } = {}) {
    const generation = ++this.generation
    if (!navigator.mediaDevices?.getUserMedia) throw new Error('当前浏览器不支持麦克风访问，请使用 localhost 或 HTTPS。')
    const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } })
    if (generation !== this.generation) { stream.getTracks().forEach(track => track.stop()); return }
    this.stream = stream
    try {
      const context = new AudioContext({ sampleRate: 16000, latencyHint: 'interactive' }); this.context = context
      await context.audioWorklet.addModule('/pcm-recorder.js')
      if (generation !== this.generation) return
      const node = new AudioWorkletNode(context, 'harness-pcm'); this.node = node
      const encoder = new PCM16k(context.sampleRate)
      const vad = new UtteranceVAD(this.onSpeech, (samples, rate, endedAt) => {
        if (options.sink) options.sink.commit(endedAt)
        else this.onUtterance({ audio_base64: wav(samples, rate), sample_rate: rate, channels: 1, format: 'wav', mime_type: 'audio/wav' }, endedAt)
      }, { silenceMS: options.silenceMS, onsetMS: options.onsetMS, discard: options.sink?.discard, audio: options.sink ? samples => options.sink!.audio(encoder.feed(samples)) : undefined })
      this.vad = vad
      node.port.onmessage = event => {
        if (generation !== this.generation) return
        try { this.onLevel(vad.feed(new Float32Array(event.data), context.sampleRate, performance.now())) }
        catch (error) { options.onError?.(error); void this.stop() }
      }
      this.source = context.createMediaStreamSource(stream); this.source.connect(node); node.connect(context.destination)
      await context.resume()
    } catch (error) { await this.stop(); throw error }
  }
  discard() { this.vad?.discard() }
  async stop() {
    this.generation++; this.stream?.getTracks().forEach(track => track.stop()); this.node?.disconnect(); this.source?.disconnect()
    if (this.context && this.context.state !== 'closed') await this.context.close()
    this.context = undefined; this.node = undefined; this.stream = undefined; this.vad = undefined; this.onLevel(0)
  }
}
export function deviceError(error: unknown, name: string): string {
  const value = error as { name?: string; message?: string }
  if (value?.name === 'NotAllowedError') return `${name}权限未允许，请在浏览器授权后重试。`
  if (value?.name === 'NotFoundError') return `未找到可用的${name}。`
  if (value?.name === 'NotReadableError') return `${name}无法读取，可能被其他应用占用。`
  return `${name}启动失败：${value?.message ?? String(error)}`
}
