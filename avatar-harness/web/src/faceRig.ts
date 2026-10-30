import { ARKIT_NAMES } from './arkit.ts'

export const FACE_REGIONS = [
  { id: 'brows', label: '眉毛 / 额头', channels: [
    ['browInnerUp', '抬内眉'], ['browOuterUpLeft', '抬左外眉'], ['browOuterUpRight', '抬右外眉'], ['browDownLeft', '压左眉'], ['browDownRight', '压右眉'],
  ] },
  { id: 'eyes', label: '眼睑 / 视线', channels: [
    ['eyeBlinkLeft', '左眼闭合'], ['eyeBlinkRight', '右眼闭合'], ['eyeSquintLeft', '左眼眯起'], ['eyeSquintRight', '右眼眯起'], ['eyeWideLeft', '左眼睁大'], ['eyeWideRight', '右眼睁大'],
    ['eyeLookDownLeft', '左眼向下'], ['eyeLookDownRight', '右眼向下'], ['eyeLookUpLeft', '左眼向上'], ['eyeLookUpRight', '右眼向上'],
    ['eyeLookInLeft', '左眼向内'], ['eyeLookInRight', '右眼向内'], ['eyeLookOutLeft', '左眼向外'], ['eyeLookOutRight', '右眼向外'],
  ] },
  { id: 'cheeks', label: '面颊 / 鼻翼', channels: [
    ['cheekPuff', '鼓腮'], ['cheekSquintLeft', '左颊上提'], ['cheekSquintRight', '右颊上提'], ['noseSneerLeft', '左鼻翼提起'], ['noseSneerRight', '右鼻翼提起'],
  ] },
  { id: 'mouth', label: '嘴唇 / 嘴角', channels: [
    ['mouthClose', '闭唇'], ['mouthFunnel', '漏斗嘴'], ['mouthPucker', '撅嘴'], ['mouthLeft', '嘴部向左'], ['mouthRight', '嘴部向右'],
    ['mouthSmileLeft', '左嘴角上扬'], ['mouthSmileRight', '右嘴角上扬'], ['mouthFrownLeft', '左嘴角下压'], ['mouthFrownRight', '右嘴角下压'],
    ['mouthDimpleLeft', '左嘴角收紧'], ['mouthDimpleRight', '右嘴角收紧'], ['mouthStretchLeft', '左唇横向拉伸'], ['mouthStretchRight', '右唇横向拉伸'],
    ['mouthUpperUpLeft', '左上唇提起'], ['mouthUpperUpRight', '右上唇提起'], ['mouthLowerDownLeft', '左下唇下拉'], ['mouthLowerDownRight', '右下唇下拉'],
    ['mouthPressLeft', '左唇压紧'], ['mouthPressRight', '右唇压紧'], ['mouthRollLower', '下唇内收'], ['mouthRollUpper', '上唇内收'], ['mouthShrugLower', '下唇上提'], ['mouthShrugUpper', '上唇上提'],
  ] },
  { id: 'jaw', label: '下颌 / 舌', channels: [
    ['jawOpen', '张颌'], ['jawForward', '下颌前伸'], ['jawLeft', '下颌向左'], ['jawRight', '下颌向右'], ['tongueOut', '伸舌'],
  ] },
] as const

export interface FaceRigSnapshot { supported: string[]; weights: Record<string, number> }
export const BROW_ACTIONS: Record<string, Record<string, number>> = {
  raise: { browInnerUp: .9, browOuterUpLeft: 1, browOuterUpRight: 1, browDownLeft: 0, browDownRight: 0 },
  inner: { browInnerUp: 1, browOuterUpLeft: 0, browOuterUpRight: 0, browDownLeft: 0, browDownRight: 0 },
  frown: { browInnerUp: 0, browOuterUpLeft: 0, browOuterUpRight: 0, browDownLeft: 1, browDownRight: 1 },
}
const names = new Set<string>(ARKIT_NAMES)

export class FaceRigControl {
  private available = new Set<string>()
  private values: Record<string, number> = {}
  get overrides() { return { ...this.values } }
  setSupported(supported: string[]) {
    this.available = new Set(supported.filter(name => names.has(name)))
    for (const name of Object.keys(this.values)) if (!this.available.has(name)) delete this.values[name]
  }
  set(name: string, value: number) {
    if (!this.available.has(name) || !Number.isFinite(value) || value < 0 || value > 1) return false
    this.values[name] = value
    return true
  }
  browAction(action: string) {
    const pose = BROW_ACTIONS[action]
    if (!pose) return false
    for (const [name, value] of Object.entries(pose)) this.set(name, value)
    return true
  }
  reset(region?: string) {
    if (!region) { this.values = {}; return }
    for (const group of FACE_REGIONS) if (group.id === region) for (const [name] of group.channels) delete this.values[name]
  }
}
