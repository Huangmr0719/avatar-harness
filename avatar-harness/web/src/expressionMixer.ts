import { ARKIT_NAMES, arkitFrame } from './arkit.ts'
import type { ExpressionStyle } from './expressionControl.ts'

// Project-authored poses in the official LAM names/weights coordinate order.
// Phonetic channels (jaw, closure, funnel, pucker, tongue) stay with audio.
const poses: Record<ExpressionStyle, Record<string, number>> = {
  neutral: {},
  natural: {},
  happy: { mouthSmileLeft: .5, mouthSmileRight: .5, cheekSquintLeft: .16, cheekSquintRight: .16, eyeSquintLeft: .1, eyeSquintRight: .1, browOuterUpLeft: .06, browOuterUpRight: .06 },
  sad: { browInnerUp: .55, browDownLeft: .15, browDownRight: .15, mouthFrownLeft: .35, mouthFrownRight: .35 },
  angry: { browDownLeft: .65, browDownRight: .65, eyeSquintLeft: .3, eyeSquintRight: .3, noseSneerLeft: .12, noseSneerRight: .12 },
  fearful: { browInnerUp: .6, browOuterUpLeft: .3, browOuterUpRight: .3, browDownLeft: .18, browDownRight: .18, eyeWideLeft: .55, eyeWideRight: .55, mouthStretchLeft: .15, mouthStretchRight: .15 },
  surprised: { browInnerUp: .45, browOuterUpLeft: .65, browOuterUpRight: .65, eyeWideLeft: .65, eyeWideRight: .65 },
  disgusted: { noseSneerLeft: .65, noseSneerRight: .65, mouthUpperUpLeft: .3, mouthUpperUpRight: .3, browDownLeft: .22, browDownRight: .22, cheekSquintLeft: .2, cheekSquintRight: .2 },
  focused: { browDownLeft: .24, browDownRight: .24, eyeSquintLeft: .1, eyeSquintRight: .1 },
  concerned: { browInnerUp: .35, browDownLeft: .08, browDownRight: .08, eyeWideLeft: .04, eyeWideRight: .04, mouthFrownLeft: .16, mouthFrownRight: .16 },
}

export class ExpressionMixer {
  private current = new Float32Array(52)
  private target = new Float32Array(52)
  private lastTime?: number
  set(style: ExpressionStyle, intensity: number, pad: Record<string, number> = {}) {
    const strength = Number.isFinite(intensity) ? Math.max(0, Math.min(1, intensity)) : 0
    const values = { ...poses[style] }
    for (const name of Object.keys(values)) values[name] *= strength
    const p = Math.max(0, Math.min(1, pad.pleasure ?? 0)), a = Math.max(0, Math.min(1, pad.arousal ?? 0))
    if (style === 'natural') { values.mouthSmileLeft = p * .25; values.mouthSmileRight = p * .25 }
    values.browOuterUpLeft = Math.max(values.browOuterUpLeft ?? 0, a * .15)
    values.browOuterUpRight = Math.max(values.browOuterUpRight ?? 0, a * .15)
    this.target = Float32Array.from(ARKIT_NAMES.map(name => values[name] ?? 0))
  }
  sample(now: number, speech: Record<string, number> = {}): Record<string, number> {
    const alpha = this.lastTime == null ? 1 : 1 - Math.exp(-Math.max(0, now - this.lastTime) / 120)
    this.lastTime = now
    for (let i = 0; i < 52; i++) this.current[i] += (this.target[i] - this.current[i]) * alpha
    const values = arkitFrame(this.current)
    for (const name of ARKIT_NAMES) {
      const raw = speech[name], base = Number.isFinite(raw) ? Math.max(0, Math.min(1, raw)) : 0
      values[name] = base + (1 - base) * values[name]
    }
    return values
  }
}
