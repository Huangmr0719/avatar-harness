import { GaussianSplatRenderer } from '@myned-ai/gsplat-flame-avatar-renderer'
import { Euler, Quaternion, Vector3, type Object3D, type PerspectiveCamera } from 'three'
import { AvatarTimeline } from './avatarTimeline'
import { LamSpeech } from './lamSpeech'
import type { AudioSegment, MotionFrame } from './types'
import type { ExpressionStyle } from './expressionControl'
import { applyBlink, exaggerateExpression, ExpressionMixer } from './expressionMixer'
import { HEAD_AXES, type HeadOverrides, type FaceRigSnapshot } from './faceRig'
import { ARKIT_NAMES } from './arkit'
import { Blink } from './blink'
import { AttentionMotion, type AttentionPose } from './attentionMotion'
import { disposeLamRenderer } from './rendererLifecycle'
import { calibrateSpeechJaw } from './articulationCalibration'

const SPEECH_MOTION_RELEASE_MS = 200

export class LamView {
  private renderer?: GaussianSplatRenderer
  private timeline = new AvatarTimeline()
  private speech: LamSpeech
  private disposed = false
  private loading?: Promise<void>
  private cleanup?: Promise<void>
  private segments: AudioSegment[] = []
  private mixer = new ExpressionMixer()
  private blink = new Blink()
  private attention = new AttentionMotion()
  private attentionPose: AttentionPose = { active: 0, gaze: { yawDeg: 0, pitchDeg: 0 }, head: [0, 0, 0] }
  private listening = false
  private userSpeaking = false
  private headOverrides: HeadOverrides = {}
  private headBone?: Object3D
  private headBase = new Quaternion()
  private restoreHead = false
  private faceOverrides: Record<string, number> = {}
  private comparison = false
  private expressionStyle: ExpressionStyle = 'neutral'
  private lastDraw: { at_ms: number; audio_pts_ms: number; frame_pts_ms: number } | null = null
  constructor(private host: HTMLElement, private onStatus: (state: string, message?: string) => void,
    private onDriver: (state: string, message?: string) => void, private pad: Record<string, number> = {}, remoteURL?: string, private speechJawMax = 1) {
    this.speech = new LamSpeech(remoteURL)
    this.onDriver('loading', remoteURL ? '正在连接 data2 语音驱动' : '正在加载本地语音表情模型')
    this.speech.ready.then(() => { if (!this.disposed) this.onDriver('ready', `${remoteURL ? '远程 LAM-A2E' : '本地 Wav2ARKit'} · ${this.speech.backend.toUpperCase()}`) }).catch(error => { if (!this.disposed) this.onDriver('error', String(error)) })
  }
  load(url: string): Promise<void> {
    if (this.disposed) return Promise.resolve()
    this.loading = this.loadRenderer(url)
    return this.loading
  }
  private async loadRenderer(url: string) {
    try {
      // create() is present in 1.4.0; upstream's declaration still only lists
      // its deprecated singleton alias. Keep the correction local.
      const factory = GaussianSplatRenderer as typeof GaussianSplatRenderer & {
        create: typeof GaussianSplatRenderer.getInstance
      }
      const options = {
        backgroundColor: '0x111b23',
        // Brief PCM gaps should not restart the SDK's head/body animation.
        // Facial articulation still follows the exact playback interval.
        getChatState: () => {
          // Undo our previous delta BEFORE the clip mixer runs. Constant clip
          // keys can skip a bone write; without this, deltas would accumulate.
          if (this.restoreHead && this.headBone) this.headBone.quaternion.copy(this.headBase)
          this.restoreHead = false
          return !this.comparison && this.isSpeaking(SPEECH_MOTION_RELEASE_MS) ? 'Responding' : 'Idle'
        },
        getExpressionData: () => this.expression(),
        getNeckPose: () => {
          if (this.comparison) return null
          this.headBone ??= this.rigRoot()?.getObjectByName('head')
          if (!this.headBone) return null
          this.headBase.copy(this.headBone.quaternion); this.restoreHead = true
          return { head: this.attentionPose.head.map((value, index) => this.headOverrides[HEAD_AXES[index].id] == null ? value : this.headOverrides[HEAD_AXES[index].id]! * Math.PI / 180) as [number, number, number] }
        },
        // Manual eye controls own the entire gaze pair while being adjusted.
        getGazeOffset: () => !this.comparison && this.attentionPose.active > .01 && !Object.keys(this.faceOverrides).some(name => name.startsWith('eyeLook')) ? this.attentionPose.gaze : null,
      }
      const renderer = await factory.create(this.host as HTMLDivElement, url, options)
      this.renderer = renderer
      if (this.disposed) return
      renderer.viewer.renderer.setClearAlpha(0)
      this.reset()
      this.onStatus('ready')
    } catch (error) { if (!this.disposed) this.onStatus('error', String(error)) }
  }
  private isSpeaking(releaseMS = 0) { const now = performance.now(); return this.segments.some(s => now >= s.start_ms && now < s.start_ms + s.duration_ms + releaseMS) }
  setExpression(style: ExpressionStyle, intensity = 1) { this.expressionStyle = style; this.mixer.set(style, intensity, this.comparison ? {} : this.pad) }
  setComparison(active: boolean) { this.comparison = active }
  setAttention(listening: boolean, speaking: boolean) { this.listening = listening; this.userSpeaking = speaking }
  async reconnectDriver() {
    this.onDriver('loading', '正在重连口型驱动')
    this.clearMotion()
    try {
      await this.speech.reconnect()
      if (!this.disposed) this.onDriver('ready', `LAM-A2E · ${this.speech.backend.toUpperCase()}`)
    } catch (error) { if (!this.disposed) this.onDriver('error', String(error)); throw error }
  }
  setHeadOverrides(values: HeadOverrides) {
    this.headOverrides = Object.fromEntries(HEAD_AXES.filter(axis => Number.isFinite(values[axis.id]) && values[axis.id]! >= axis.min && values[axis.id]! <= axis.max).map(axis => [axis.id, values[axis.id]]))
  }
  private rigRoot() { return (this.renderer?.viewer as unknown as { boneRoot?: Object3D } | undefined)?.boneRoot }
  setFaceOverrides(values: Record<string, number>) {
    this.faceOverrides = Object.fromEntries(ARKIT_NAMES.filter(name => Number.isFinite(values[name]) && values[name] >= 0 && values[name] <= 1).map(name => [name, values[name]]))
  }
  faceState(): FaceRigSnapshot {
    const mesh = this.renderer?.viewer.splatMesh as unknown as { morphTargetDictionary?: Record<string, number>; bsWeight?: Record<string, number> } | undefined
    const head = this.rigRoot()?.getObjectByName('head')
    const rotation = head ? new Euler().setFromQuaternion(head.quaternion, 'YXZ') : undefined
    return { supported: Object.keys(mesh?.morphTargetDictionary ?? {}).filter(name => ARKIT_NAMES.includes(name as typeof ARKIT_NAMES[number])), weights: { ...mesh?.bsWeight }, headSupported: !!head, headPose: rotation ? { pitch: rotation.x * 180 / Math.PI, yaw: rotation.y * 180 / Math.PI, roll: rotation.z * 180 / Math.PI } : {} }
  }
  private expression() {
    const now = performance.now(), sample = this.isSpeaking() ? this.timeline.sample(now) : null
    this.attentionPose = this.attention.sample(now, this.listening && !this.comparison && !sample, this.userSpeaking)
    const values = this.mixer.sample(now, sample?.frame.blendshapes, sample?.frame.speech_energy, { speechAffect: !this.comparison, animateAffect: !this.comparison, speaking: !!sample })
    applyBlink(values, this.blink.sample(now, Math.max(values.eyeBlinkLeft, values.eyeBlinkRight)))
    if (this.comparison) exaggerateExpression(values, this.expressionStyle)
    if (sample) calibrateSpeechJaw(values, this.speechJawMax)
    Object.assign(values, this.faceOverrides)
    // This callback runs inside the SDK render loop. Publish after the same
    // frame has drawn, only when real inferred motion and audio are active.
    if (sample) queueMicrotask(() => {
      if (!this.disposed && this.renderer?.viewer.splatRenderReady) this.lastDraw = { at_ms: performance.now(), audio_pts_ms: sample.audio_pts_ms, frame_pts_ms: sample.frame.pts_ms ?? 0 }
    })
    return values
  }
  async prepare(buffer: AudioBuffer, pts: number) {
    const start = performance.now()
    let frames: MotionFrame[]
    try { frames = await this.speech.prepare(buffer, pts) }
    catch (error) { if (!this.disposed) { this.clearMotion(); this.onDriver('error', String(error)) }; throw error }
    if (this.disposed || !frames.length) return
    this.timeline.addFrames(frames)
    return { elapsed_ms: performance.now() - start }
  }
  addFrames(frames: MotionFrame[], base: number) { this.timeline.addFrames(frames, base) }
  addAudio(segment: AudioSegment) {
    this.timeline.addAudio(segment)
    this.segments = this.segments.filter(s => s.start_ms + s.duration_ms + SPEECH_MOTION_RELEASE_MS > performance.now())
    this.segments.push(segment)
  }
  clearMotion() { this.speech.reset(); this.timeline.reset(); this.segments = []; this.lastDraw = null }
  probe() { return this.lastDraw }
  reset() {
    if (!this.renderer) return
    const camera = this.renderer.getCamera() as PerspectiveCamera
    // Frame the face from eye height. A fixed 1.65 m camera sits below this
    // rig's eyes and adds a low-angle view to the clip's head motion.
    const root = this.rigRoot()
    root?.updateWorldMatrix(true, true)
    const eyeHeights = ['lEye', 'rEye'].flatMap(name => {
      const eye = root?.getObjectByName(name)
      return eye ? [eye.getWorldPosition(new Vector3()).y] : []
    }).filter(Number.isFinite)
    const eyeY = eyeHeights.length ? eyeHeights.reduce((a, b) => a + b, 0) / eyeHeights.length : 1.68
    const framingY = eyeY - .03
    camera.position.set(0, eyeY, .72); camera.lookAt(0, framingY, 0)
    camera.fov = 32; camera.updateProjectionMatrix()
    const controls = (this.renderer.viewer as any).controls
    controls?.target.set(0, framingY, 0); controls?.update()
  }
  dispose(): Promise<void> {
    if (this.cleanup) return this.cleanup
    this.disposed = true; this.clearMotion(); this.speech.dispose()
    this.cleanup = (async () => {
      await this.loading
      if (this.renderer) await disposeLamRenderer(this.renderer)
      this.renderer = undefined
    })()
    return this.cleanup
  }
}
