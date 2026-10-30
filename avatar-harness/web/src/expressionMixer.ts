import { ARKIT_NAMES, arkitFrame } from './arkit.ts'
import type { ExpressionStyle } from './expressionControl.ts'

// Project-authored affect poses for the LAM rig. Audio inference owns phonetic
// articulation; the LLM's affect explicitly coordinates the rest of the face.
const poses: Record<ExpressionStyle, Record<string, number>> = {
  neutral: {},
  natural: {},
  happy: { mouthSmileLeft: .95, mouthSmileRight: .95, cheekSquintLeft: 1, cheekSquintRight: 1, eyeSquintLeft: .8, eyeSquintRight: .8, browInnerUp: .3, browOuterUpLeft: .75, browOuterUpRight: .75 },
  sad: { browInnerUp: 1, browDownLeft: .5, browDownRight: .5, eyeSquintLeft: .42, eyeSquintRight: .42, eyeBlinkLeft: .12, eyeBlinkRight: .12, eyeLookDownLeft: .2, eyeLookDownRight: .2, mouthFrownLeft: .8, mouthFrownRight: .8 },
  angry: { browDownLeft: 1, browDownRight: 1, eyeSquintLeft: .85, eyeSquintRight: .85, noseSneerLeft: .35, noseSneerRight: .35, mouthFrownLeft: .3, mouthFrownRight: .3 },
  fearful: { browInnerUp: 1, browOuterUpLeft: .7, browOuterUpRight: .7, browDownLeft: .35, browDownRight: .35, eyeWideLeft: 1, eyeWideRight: 1, mouthStretchLeft: .45, mouthStretchRight: .45 },
  surprised: { browInnerUp: 1, browOuterUpLeft: 1, browOuterUpRight: 1, eyeWideLeft: 1, eyeWideRight: 1 },
  disgusted: { noseSneerLeft: 1, noseSneerRight: 1, mouthUpperUpLeft: .65, mouthUpperUpRight: .65, browDownLeft: .8, browDownRight: .8, eyeSquintLeft: .7, eyeSquintRight: .7, cheekSquintLeft: .6, cheekSquintRight: .6 },
  focused: { browDownLeft: .24, browDownRight: .24, eyeSquintLeft: .1, eyeSquintRight: .1 },
  concerned: { browInnerUp: .35, browDownLeft: .08, browDownRight: .08, eyeWideLeft: .04, eyeWideRight: .04, mouthFrownLeft: .16, mouthFrownRight: .16 },
}

// Small measured-energy accents, not emotion recognition or random face motion.
const accents: Record<ExpressionStyle, Record<string, number>> = {
  neutral: { browInnerUp: .5, browOuterUpLeft: .65, browOuterUpRight: .65, eyeWideLeft: .06, eyeWideRight: .06 },
  natural: { browInnerUp: .5, browOuterUpLeft: .65, browOuterUpRight: .65 },
  happy: { browOuterUpLeft: .16, browOuterUpRight: .16, cheekSquintLeft: .08, cheekSquintRight: .08 },
  sad: { browInnerUp: .1, eyeSquintLeft: .04, eyeSquintRight: .04 },
  angry: { browDownLeft: .12, browDownRight: .12, eyeSquintLeft: .08, eyeSquintRight: .08 },
  fearful: { browInnerUp: .12, eyeWideLeft: .08, eyeWideRight: .08 },
  surprised: { browOuterUpLeft: .15, browOuterUpRight: .15, eyeWideLeft: .1, eyeWideRight: .1 },
  disgusted: { noseSneerLeft: .1, noseSneerRight: .1, cheekSquintLeft: .06, cheekSquintRight: .06 },
  focused: { browDownLeft: .12, browDownRight: .12 },
  concerned: { browInnerUp: .12 },
}
const articulation = new Set<string>([
  'jawForward', 'jawLeft', 'jawOpen', 'jawRight', 'mouthClose', 'mouthFunnel',
  'mouthPucker', 'mouthLeft', 'mouthRight', 'mouthLowerDownLeft', 'mouthLowerDownRight',
  'mouthRollLower', 'mouthRollUpper', 'mouthShrugLower', 'mouthShrugUpper',
  'mouthPressLeft', 'mouthPressRight', 'tongueOut',
])
const clamp = (value: number) => Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : 0

// Presentation-only extrapolation of existing morphs. Keep phonetic controls,
// gaze and blink closure in their normal range; lids get a lower ceiling.
const theatricalGains: Record<string, [number, number]> = {
  browInnerUp: [1.8, 1.65], browOuterUpLeft: [1.8, 1.65], browOuterUpRight: [1.8, 1.65],
  browDownLeft: [1.8, 1.65], browDownRight: [1.8, 1.65],
  cheekSquintLeft: [1.35, 1.4], cheekSquintRight: [1.35, 1.4],
  eyeSquintLeft: [1.35, 1.15], eyeSquintRight: [1.35, 1.15],
  eyeWideLeft: [1.25, 1.25], eyeWideRight: [1.25, 1.25],
  mouthSmileLeft: [1.5, 1.45], mouthSmileRight: [1.5, 1.45],
  mouthFrownLeft: [1.5, 1.4], mouthFrownRight: [1.5, 1.4],
  mouthStretchLeft: [1.35, 1.2], mouthStretchRight: [1.35, 1.2],
  mouthUpperUpLeft: [1.2, 1.2], mouthUpperUpRight: [1.2, 1.2],
  noseSneerLeft: [1.5, 1.5], noseSneerRight: [1.5, 1.5],
}
export function exaggerateExpression(values: Record<string, number>, style: ExpressionStyle) {
  if (style === 'neutral' || style === 'natural') return values
  for (const [name, [gain, maximum]] of Object.entries(theatricalGains)) {
    values[name] = Math.min(maximum, clamp(values[name]) * gain)
  }
  return values
}

// Closure wins over lid-raising/squinting, including during surprise/fear.
export function applyBlink(values: Record<string, number>, blink: number) {
  for (const side of ['Left', 'Right']) {
    const closure = Math.max(clamp(values['eyeBlink' + side]), clamp(blink))
    values['eyeBlink' + side] = closure
    values['eyeWide' + side] = clamp(values['eyeWide' + side]) * (1 - closure)
    values['eyeSquint' + side] = clamp(values['eyeSquint' + side]) * (1 - closure)
  }
  return values
}

export class ExpressionMixer {
  private current = new Float32Array(52)
  private target = new Float32Array(52)
  private lastTime?: number
  private style: ExpressionStyle = 'neutral'
  private strength = 0
  private energy = 0
  private accent = 0
  private lastAccent = -Infinity
  set(style: ExpressionStyle, intensity: number, pad: Record<string, number> = {}) {
    const requested = clamp(intensity)
    // Boost readable mid-range affect while preserving zero, full scale and
    // ordering. Explicitly subtle requests still approach zero continuously.
    const strength = style === 'neutral' || style === 'natural' ? requested : requested * (1.7 - .7 * requested)
    this.style = strength <= .05 ? 'neutral' : style; this.strength = strength
    const values = { ...poses[style] }
    for (const name of Object.keys(values)) values[name] *= strength
    const p = Math.max(0, Math.min(1, pad.pleasure ?? 0)), a = Math.max(0, Math.min(1, pad.arousal ?? 0))
    if (style === 'natural') { values.mouthSmileLeft = p * .25 * strength; values.mouthSmileRight = p * .25 * strength }
    values.browOuterUpLeft = Math.max(values.browOuterUpLeft ?? 0, a * .15 * strength)
    values.browOuterUpRight = Math.max(values.browOuterUpRight ?? 0, a * .15 * strength)
    this.target = Float32Array.from(ARKIT_NAMES.map(name => values[name] ?? 0))
  }
  sample(now: number, speech: Record<string, number> = {}, speechEnergy = 0, options: { speechAffect?: boolean } = {}): Record<string, number> {
    const delta = this.lastTime == null ? 16 : Math.max(0, now - this.lastTime)
    const alpha = this.lastTime == null ? 1 : 1 - Math.exp(-delta / 120)
    const energy = options.speechAffect === false ? 0 : clamp(speechEnergy)
    if (options.speechAffect === false) { this.accent = 0; this.energy = 0 }
    this.accent *= Math.exp(-delta / 360)
    if (energy > .55 && energy - this.energy > .12 && now - this.lastAccent >= 900) {
      this.accent = energy; this.lastAccent = now
    }
    this.energy += (energy - this.energy) * (1 - Math.exp(-delta / 90))
    this.lastTime = now
    for (let i = 0; i < 52; i++) {
      const emphasis = (accents[this.style][ARKIT_NAMES[i]] ?? 0) * this.accent * (.4 + .6 * this.strength)
      this.current[i] += (clamp(this.target[i] + emphasis) - this.current[i]) * alpha
    }
    const values = arkitFrame(this.current)
    for (const name of ARKIT_NAMES) {
      const base = clamp(speech[name])
      // Preserve articulation exactly. The requested affect owns the remaining
      // face, instead of union-mixing arbitrary audio smile/frown/lid guesses.
      if (articulation.has(name)) values[name] = base
      else {
        const residual = options.speechAffect === false ? 0 : this.style !== 'neutral' && this.style !== 'natural' && this.strength > .05 ? .15 : 1
        values[name] = values[name] + (1 - values[name]) * base * residual
      }
    }
    return values
  }
}
