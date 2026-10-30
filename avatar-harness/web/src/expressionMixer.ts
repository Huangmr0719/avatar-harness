import { ARKIT_NAMES, arkitFrame } from './arkit.ts'
import type { ExpressionStyle } from './expressionControl.ts'

// Project-authored affect poses, calibrated on the actual LAM rig. The audio
// export has near-zero brow/eyelid output; 52 outputs do not imply full-face SER.
const poses: Record<ExpressionStyle, Record<string, number>> = {
  neutral: {},
  natural: {},
  happy: { mouthSmileLeft: .6, mouthSmileRight: .6, cheekSquintLeft: .9, cheekSquintRight: .9, eyeSquintLeft: .6, eyeSquintRight: .6, browInnerUp: .22, browOuterUpLeft: .5, browOuterUpRight: .5 },
  sad: { browInnerUp: .85, browDownLeft: .32, browDownRight: .32, eyeSquintLeft: .16, eyeSquintRight: .16, mouthFrownLeft: .5, mouthFrownRight: .5 },
  angry: { browDownLeft: .9, browDownRight: .9, eyeSquintLeft: .55, eyeSquintRight: .55, noseSneerLeft: .18, noseSneerRight: .18 },
  fearful: { browInnerUp: .85, browOuterUpLeft: .4, browOuterUpRight: .4, browDownLeft: .25, browDownRight: .25, eyeWideLeft: .8, eyeWideRight: .8, mouthStretchLeft: .25, mouthStretchRight: .25 },
  surprised: { browInnerUp: .75, browOuterUpLeft: .95, browOuterUpRight: .95, eyeWideLeft: .95, eyeWideRight: .95 },
  disgusted: { noseSneerLeft: .9, noseSneerRight: .9, mouthUpperUpLeft: .42, mouthUpperUpRight: .42, browDownLeft: .45, browDownRight: .45, cheekSquintLeft: .35, cheekSquintRight: .35 },
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
    const strength = Number.isFinite(intensity) ? Math.max(0, Math.min(1, intensity)) : 0
    this.style = strength <= .05 ? 'neutral' : style; this.strength = strength
    const values = { ...poses[style] }
    for (const name of Object.keys(values)) values[name] *= strength
    const p = Math.max(0, Math.min(1, pad.pleasure ?? 0)), a = Math.max(0, Math.min(1, pad.arousal ?? 0))
    if (style === 'natural') { values.mouthSmileLeft = p * .25; values.mouthSmileRight = p * .25 }
    values.browOuterUpLeft = Math.max(values.browOuterUpLeft ?? 0, a * .15)
    values.browOuterUpRight = Math.max(values.browOuterUpRight ?? 0, a * .15)
    this.target = Float32Array.from(ARKIT_NAMES.map(name => values[name] ?? 0))
  }
  sample(now: number, speech: Record<string, number> = {}, speechEnergy = 0): Record<string, number> {
    const delta = this.lastTime == null ? 16 : Math.max(0, now - this.lastTime)
    const alpha = this.lastTime == null ? 1 : 1 - Math.exp(-delta / 120)
    const energy = clamp(speechEnergy)
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
        const residual = this.style !== 'neutral' && this.style !== 'natural' && this.strength > .05 ? .15 : 1
        values[name] = values[name] + (1 - values[name]) * base * residual
      }
    }
    return values
  }
}
