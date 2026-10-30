import * as THREE from 'three'
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js'
import { OrbitControls } from 'three/addons/controls/OrbitControls.js'
import { VRMLoaderPlugin, VRMUtils, type VRM } from '@pixiv/three-vrm'
import { AvatarTimeline } from './avatarTimeline'
import type { AudioSegment, MotionFrame } from './types'
import { animateChengIdle, isChengAvatar } from './characters/cheng'

export class AvatarView {
  private renderer: THREE.WebGLRenderer
  private scene = new THREE.Scene()
  private camera = new THREE.PerspectiveCamera(30, 1, .01, 100)
  private controls: OrbitControls
  private vrm?: VRM
  private model?: THREE.Object3D
  private procedural = false
  private elapsed = 0
  private morphs: THREE.Mesh[] = []
  private timeline = new AvatarTimeline()
  private observer: ResizeObserver
  private animation = 0
  private disposed = false
  private clock = new THREE.Clock()
  private abort = new AbortController()
  private home = { target: new THREE.Vector3(), camera: new THREE.Vector3(0, 1.4, 2.2) }
  private lastDraw: { at_ms: number; audio_pts_ms: number; frame_pts_ms: number } | null = null

  constructor(private host: HTMLElement, private onStatus: (state: string, message?: string) => void) {
    this.renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true, powerPreference: 'high-performance' })
    this.renderer.setPixelRatio(Math.min(devicePixelRatio, 1.5))
    this.renderer.outputColorSpace = THREE.SRGBColorSpace
    this.renderer.toneMapping = THREE.ACESFilmicToneMapping
    this.renderer.toneMappingExposure = 1.2
    this.host.appendChild(this.renderer.domElement)
    this.scene.add(new THREE.HemisphereLight(0xffffff, 0x5b6474, 2.1))
    const key = new THREE.DirectionalLight(0xffffff, 2.2); key.position.set(1, 2, 3); this.scene.add(key)
    const rim = new THREE.DirectionalLight(0x9fbce0, 1.1); rim.position.set(-2, 2, -1); this.scene.add(rim)
    this.controls = new OrbitControls(this.camera, this.renderer.domElement)
    this.controls.enableDamping = true; this.controls.enablePan = false; this.controls.minDistance = .2; this.controls.maxDistance = 8
    this.observer = new ResizeObserver(() => this.resize()); this.observer.observe(this.host); this.resize()
    this.animate()
  }
  async load(url: string) {
    this.onStatus('loading')
    try {
      const response = await fetch(url, { signal: this.abort.signal })
      if (!response.ok) throw new Error(`形象资源 HTTP ${response.status}`)
      const bytes = await response.arrayBuffer()
      const loader = new GLTFLoader(); loader.register(parser => new VRMLoaderPlugin(parser))
      const base = url.startsWith('blob:') ? location.href : new URL('.', new URL(url, location.href)).href
      const gltf = await loader.parseAsync(bytes, base)
      if (this.disposed) { VRMUtils.deepDispose(gltf.scene); return }
      this.vrm = gltf.userData.vrm
      this.model = this.vrm?.scene ?? gltf.scene
      this.procedural = isChengAvatar(this.model)
      if (this.vrm) {
        VRMUtils.rotateVRM0(this.vrm)
        const left = this.vrm.humanoid.getNormalizedBoneNode('leftUpperArm')
        const right = this.vrm.humanoid.getNormalizedBoneNode('rightUpperArm')
        if (left) left.rotation.z = -1.15
        if (right) right.rotation.z = 1.15
        this.vrm.update(0)
      }
      this.scene.add(this.model)
      this.model.traverse(node => { if ((node as THREE.Mesh).isMesh) this.morphs.push(node as THREE.Mesh) })
      this.model.updateMatrixWorld(true)
      const bounds = new THREE.Box3().setFromObject(this.model)
      const height = Math.max(bounds.max.y - bounds.min.y, .1)
      const center = bounds.getCenter(new THREE.Vector3())
      this.home.target.set(center.x, bounds.max.y - height * (this.vrm ? .22 : .28), center.z)
      this.home.camera.set(center.x, this.home.target.y, center.z + height * (this.vrm ? .94 : 1.12))
      if (this.procedural) {
        this.home.target.set(center.x, bounds.min.y + height * .62, center.z)
        this.home.camera.set(center.x, this.home.target.y, center.z + height * 2.05)
      }
      this.reset(); this.onStatus('ready')
    } catch (error) { if (!this.disposed) this.onStatus('error', String(error)) }
  }
  addFrames(frames: MotionFrame[], base: number) { this.timeline.addFrames(frames, base) }
  addAudio(segment: AudioSegment) { this.timeline.addAudio(segment) }
  clearMotion() { this.timeline.reset(); this.lastDraw = null; this.apply({ blendshapes: {}, expressions: {} }, true); if (this.procedural) this.model?.getObjectByName('Head')?.quaternion.identity() }
  probe() { return this.lastDraw }
  reset() { this.camera.position.copy(this.home.camera); this.controls.target.copy(this.home.target); this.controls.update() }
  private apply(frame: MotionFrame, clear = false) {
    let applied = false
    const values = new Map(Object.entries(frame.blendshapes ?? {}).filter(([, value]) => Number.isFinite(value)).map(([name, value]) => [name.toLowerCase().replace(/[^a-z0-9]/g, ''), Math.min(1, Math.max(0, value))]))
    for (const mesh of this.morphs) {
      if (!mesh.morphTargetDictionary || !mesh.morphTargetInfluences) continue
      for (const [name, index] of Object.entries(mesh.morphTargetDictionary)) {
        const value = values.get(name.toLowerCase().replace(/[^a-z0-9]/g, ''))
        if (clear || value != null) { mesh.morphTargetInfluences[index] = clear ? 0 : value!; applied = true }
      }
    }
    const manager = this.vrm?.expressionManager
    if (manager) {
      if (clear) manager.resetValues()
      else {
        const aliases: Record<string, string> = { jawopen: 'aa', mouthsmileleft: 'happy', mouthsmileright: 'happy', eyblinkleft: 'blinkLeft', eyblinkright: 'blinkRight', eyeblinkleft: 'blinkLeft', eyeblinkright: 'blinkRight' }
        for (const [name, value] of values) { const key = manager.getExpression(name) ? name : aliases[name]; if (key && manager.getExpression(key)) { manager.setValue(key, value); applied = true } }
        for (const [name, value] of Object.entries(frame.expressions ?? {})) if (Number.isFinite(value) && manager.getExpression(name)) { manager.setValue(name, value); applied = true }
      }
    }
    if (frame.head_quaternion?.length === 4 && frame.head_quaternion.every(Number.isFinite)) {
      const head = this.vrm?.humanoid.getNormalizedBoneNode('head') ?? this.model?.getObjectByName('Head')
      if (head) { head.quaternion.fromArray(frame.head_quaternion).normalize(); applied = true }
    }
    return applied
  }
  private resize() {
    const width = this.host.clientWidth, height = this.host.clientHeight
    if (!width || !height) return
    this.renderer.setSize(width, height); this.camera.aspect = width / height; this.camera.updateProjectionMatrix()
  }
  private animate = () => {
    if (this.disposed) return
    this.animation = requestAnimationFrame(this.animate)
    const delta = Math.min(this.clock.getDelta(), .1)
    this.elapsed += delta
    if (this.procedural && this.model) animateChengIdle(this.model, this.elapsed)
    const sample = this.timeline.sample(performance.now())
    const applied = sample ? this.apply(sample.frame) : false
    this.vrm?.update(delta); this.controls.update(); this.renderer.render(this.scene, this.camera)
    if (sample && this.model && applied) this.lastDraw = { at_ms: performance.now(), audio_pts_ms: sample.audio_pts_ms, frame_pts_ms: sample.frame.pts_ms ?? 0 }
  }
  dispose() {
    this.disposed = true; this.abort.abort(); cancelAnimationFrame(this.animation); this.observer.disconnect(); this.controls.dispose()
    if (this.model) VRMUtils.deepDispose(this.model)
    this.renderer.dispose(); this.renderer.domElement.remove()
  }
}
