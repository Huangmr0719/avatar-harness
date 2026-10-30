import type { AudioSegment } from './types'
type AudioPacket = { audio_base64: string; format?: string; sample_rate?: number; channels?: number; pts_ms?: number }

export function firstVoicedSample(buffer: AudioBuffer): number | null {
  const rate = buffer.sampleRate, window = Math.max(1, Math.round(rate * .005))
  for (let offset = 0; offset < buffer.length; offset += window) {
    const end = Math.min(buffer.length, offset + window)
    let sum = 0
    for (let channel = 0; channel < buffer.numberOfChannels; channel++) {
      const data = buffer.getChannelData(channel)
      for (let index = offset; index < end; index++) sum += data[index] * data[index]
    }
    if (Math.sqrt(sum / ((end - offset) * buffer.numberOfChannels)) >= .008) return offset / rate
  }
  return null
}

export class AudioPlayer {
  private context?: AudioContext
  private nextStart = 0
  private sources = new Set<AudioBufferSourceNode>()
  private generation = 0
  private pending = 0
  private queue: Promise<void> = Promise.resolve()
  private onPlaying: (playing: boolean) => void
  private onSchedule: (segment: AudioSegment) => void
  private onPlayback: (segment: AudioSegment) => void
  private observers = new Set<ReturnType<typeof setTimeout>>()
  private idleWaiters = new Set<() => void>()

  private prepare?: (buffer: AudioBuffer, pts: number) => Promise<void>

  constructor(onPlaying: (playing: boolean) => void = () => {}, onSchedule: (segment: AudioSegment) => void = () => {}, onPlayback: (segment: AudioSegment) => void = () => {}, prepare?: (buffer: AudioBuffer, pts: number) => Promise<void>) { this.prepare = prepare; this.onPlaying = onPlaying; this.onSchedule = onSchedule; this.onPlayback = onPlayback }

  private update() {
    const active = this.pending > 0 || this.sources.size > 0
    this.onPlaying(active)
    if (!active) for (const done of [...this.idleWaiters]) done()
  }

  // Scheduling is not completion: a comparison step awaits source.onended.
  waitUntilIdle(signal: AbortSignal): Promise<void> {
    return new Promise((resolve, reject) => {
      if (signal.aborted) { reject(new DOMException('播放已取消', 'AbortError')); return }
      if (!this.pending && !this.sources.size) { resolve(); return }
      const clean = () => { this.idleWaiters.delete(done); signal.removeEventListener('abort', abort) }
      const done = () => { clean(); resolve() }
      const abort = () => { clean(); reject(new DOMException('播放已取消', 'AbortError')) }
      this.idleWaiters.add(done)
      signal.addEventListener('abort', abort, { once: true })
    })
  }

  async unlock() {
    this.context ??= new AudioContext()
    if (this.context.state === 'suspended') await this.context.resume()
  }

  play(packet: AudioPacket): Promise<void> {
    if (!packet.audio_base64) return Promise.resolve()
    const generation = this.generation
    this.pending++
    this.update()
    const task = this.queue.then(() => this.schedule(packet, generation))
    this.queue = task.catch(() => {})
    return task.finally(() => {
      if (generation === this.generation) { this.pending--; this.update() }
    })
  }

  private async schedule(packet: AudioPacket, generation: number) {
    if (generation !== this.generation) return
    await this.unlock()
    if (generation !== this.generation) return
    const context = this.context!
    const bytes = Uint8Array.from(atob(packet.audio_base64), char => char.charCodeAt(0))
    let buffer: AudioBuffer
    if (packet.format === 'wav') {
      buffer = await context.decodeAudioData(bytes.buffer)
    } else {
      const channels = packet.channels ?? 1
      const samples = new DataView(bytes.buffer)
      const length = bytes.length / (2 * channels)
      buffer = context.createBuffer(channels, length, packet.sample_rate ?? 24000)
      for (let channel = 0; channel < channels; channel++) {
        const output = buffer.getChannelData(channel)
        for (let index = 0; index < length; index++) output[index] = samples.getInt16((index * channels + channel) * 2, true) / 32768
      }
    }
    if (generation !== this.generation) return
    await this.prepare?.(buffer, packet.pts_ms ?? 0)
    if (generation !== this.generation) return
    const source = context.createBufferSource()
    source.buffer = buffer
    source.connect(context.destination)
    const start = Math.max(context.currentTime + 0.04, this.nextStart)
    source.start(start)
    const segment = { pts_ms: packet.pts_ms ?? 0, start_ms: performance.now() + (start - context.currentTime) * 1000, duration_ms: buffer.duration * 1000 }
    this.onSchedule(segment)
    this.nextStart = start + buffer.duration
    this.sources.add(source)
    let ended = false
    source.onended = () => { ended = true; this.sources.delete(source); this.update() }
    // Observe the running sample clock after the scheduled start. Enqueuing a
    // buffer alone is never counted as a successful voice response.
    const voicedOffset = firstVoicedSample(buffer)
    if (voicedOffset == null) return
    const voicedStart = start + voicedOffset
    const observe = () => {
      if (generation !== this.generation || ended) return
      if (context.state === 'running' && context.currentTime >= voicedStart) this.onPlayback(segment)
      else if (performance.now() - segment.start_ms < 2000) later(20)
    }
    const later = (delay: number) => {
      const timer = setTimeout(() => { this.observers.delete(timer); observe() }, delay)
      this.observers.add(timer)
      // Node protocol tests do not need to keep a process alive for audio timers.
      if (typeof timer === 'object' && 'unref' in timer) (timer as { unref: () => void }).unref()
    }
    later(Math.max(0, segment.start_ms + voicedOffset * 1000 - performance.now() + 5))
    // Audio-only scheduling is deliberately not reported as synced AV playback.
  }

  stop() {
    this.generation++
    this.observers.forEach(timer => clearTimeout(timer)); this.observers.clear()
    this.sources.forEach(source => { try { source.stop() } catch { /* already ended */ } })
    this.sources.clear()
    this.nextStart = 0
    this.pending = 0
    this.queue = Promise.resolve()
    this.update()
  }
}
