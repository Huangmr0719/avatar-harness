import { GaussianSplatRenderer } from '@myned-ai/gsplat-flame-avatar-renderer'
import type { PerspectiveCamera } from 'three'
import { AvatarTimeline } from './avatarTimeline'
import { LamSpeech } from './lamSpeech'
import type { AudioSegment, MotionFrame } from './types'
import type { ExpressionStyle } from './expressionControl'
import { applyBlink, ExpressionMixer } from './expressionMixer'

export class LamView {
  private renderer?: GaussianSplatRenderer
  private timeline = new AvatarTimeline()
  private speech = new LamSpeech()
  private disposed = false
  private segments: AudioSegment[] = []
  private mixer = new ExpressionMixer()
  private lastDraw: { at_ms: number; audio_pts_ms: number; frame_pts_ms: number } | null = null
  constructor(private host: HTMLElement, private onStatus: (state: string, message?: string) => void,
    private onDriver: (state: string, message?: string) => void, private pad: Record<string, number> = {}) {
    this.onDriver('loading', '正在加载本地语音表情模型')
    this.speech.ready.then(() => { if (!this.disposed) this.onDriver('ready', `本地 Wav2ARKit · ${this.speech.backend.toUpperCase()}`) }).catch(error => { if (!this.disposed) this.onDriver('error', String(error)) })
  }
  async load(url: string) {
    try {
      // create() is present in 1.4.0; upstream's declaration still only lists
      // its deprecated singleton alias. Keep the correction local.
      const factory = GaussianSplatRenderer as typeof GaussianSplatRenderer & {
        create: typeof GaussianSplatRenderer.getInstance
      }
      const renderer = await factory.create(this.host as HTMLDivElement, url, {
        backgroundColor: '0x111b23',
        getChatState: () => this.isSpeaking() ? 'Responding' : 'Idle',
        getExpressionData: () => this.expression(),
      })
      if (this.disposed) { renderer.dispose(); return }
      this.renderer = renderer
      this.reset()
      this.onStatus('ready')
    } catch (error) { if (!this.disposed) this.onStatus('error', String(error)) }
  }
  private isSpeaking() { const now = performance.now(); return this.segments.some(s => now >= s.start_ms && now < s.start_ms + s.duration_ms) }
  setExpression(style: ExpressionStyle, intensity = 1) { this.mixer.set(style, intensity, this.pad) }
  private expression() {
    const now = performance.now(), sample = this.isSpeaking() ? this.timeline.sample(now) : null
    const values = this.mixer.sample(now, sample?.frame.blendshapes, sample?.frame.speech_energy)
    const phase = (now / 1000) % 4.3, blink = Math.max(0, 1 - Math.abs(phase - 4.05) / .1)
    applyBlink(values, blink)
    // This callback runs inside the SDK render loop. Publish after the same
    // frame has drawn, only when real inferred motion and audio are active.
    if (sample) queueMicrotask(() => {
      if (!this.disposed && this.renderer?.viewer.splatRenderReady) this.lastDraw = { at_ms: performance.now(), audio_pts_ms: sample.audio_pts_ms, frame_pts_ms: sample.frame.pts_ms ?? 0 }
    })
    return values
  }
  async prepare(buffer: AudioBuffer, pts: number) {
    const start = performance.now()
    const frames = await this.speech.prepare(buffer, pts)
    if (this.disposed || !frames.length) return
    this.timeline.addFrames(frames)
    return { elapsed_ms: performance.now() - start }
  }
  addFrames(frames: MotionFrame[], base: number) { this.timeline.addFrames(frames, base) }
  addAudio(segment: AudioSegment) {
    this.timeline.addAudio(segment)
    this.segments = this.segments.filter(s => s.start_ms + s.duration_ms > performance.now())
    this.segments.push(segment)
  }
  clearMotion() { this.speech.reset(); this.timeline.reset(); this.segments = []; this.lastDraw = null }
  probe() { return this.lastDraw }
  reset() {
    if (!this.renderer) return
    const camera = this.renderer.getCamera() as PerspectiveCamera
    camera.position.set(0, 1.65, .72); camera.lookAt(0, 1.65, 0)
    camera.fov = 32; camera.updateProjectionMatrix()
    const controls = (this.renderer.viewer as any).controls
    controls?.target.set(0, 1.65, 0); controls?.update()
  }
  dispose() { this.disposed = true; this.clearMotion(); this.speech.dispose(); this.renderer?.dispose() }
}
