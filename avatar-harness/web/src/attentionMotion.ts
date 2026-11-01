export interface AttentionPose {
  active: number
  gaze: { yawDeg: number; pitchDeg: number }
  head: [number, number, number]
}
const smooth = (t: number) => t * t * (3 - 2 * t)

// Local listening feedback follows VAD, not a claim about understanding speech.
export class AttentionMotion {
  private random: () => number
  private lastTime?: number
  private amount = 0
  private yaw = 0
  private pitch = 0
  private targetYaw = 0
  private targetPitch = 0
  private gazeAt = 0
  private away = false
  private voiced = false
  private speechAt = 0
  private nextNod = Infinity
  private nodAt = -Infinity
  private lastNod = -Infinity
  private nodPitch = 0
  constructor(random: () => number = Math.random) { this.random = random }
  sample(now: number, listening: boolean, speaking: boolean): AttentionPose {
    const delta = this.lastTime == null ? 16 : Math.min(100, Math.max(0, now - this.lastTime))
    this.lastTime = now
    this.amount += ((listening ? 1 : 0) - this.amount) * (1 - Math.exp(-delta / 300))
    if (!listening) {
      this.targetYaw = 0; this.targetPitch = 0; this.gazeAt = now
      this.voiced = false; this.nextNod = Infinity; this.nodAt = -Infinity; this.away = false
    } else {
      if (now >= this.gazeAt) {
        if (this.away) { this.targetYaw = 0; this.targetPitch = 0; this.away = false }
        else {
          this.away = this.random() > .65
          this.targetYaw = (this.random() * 2 - 1) * (this.away ? 5 : 1.6)
          this.targetPitch = this.away ? -1 - this.random() * 2 : (this.random() * 2 - 1) * .9
        }
        this.gazeAt = now + (this.away ? 500 + this.random() * 450 : 1800 + this.random() * 2200)
      }
      if (speaking && !this.voiced) { this.speechAt = now; this.nextNod = now + 2200 + this.random() * 1400 }
      if (!speaking && this.voiced && now - this.speechAt >= 550 && now - this.lastNod >= 2800) {
        this.nodAt = now + 120; this.lastNod = this.nodAt
      }
      if (speaking && now >= this.nextNod && now - this.lastNod >= 3500) {
        this.nodAt = now; this.lastNod = now; this.nextNod = now + 3800 + this.random() * 1800
      }
      this.voiced = speaking
    }
    this.yaw += (this.targetYaw - this.yaw) * (1 - Math.exp(-delta / 150))
    this.pitch += (this.targetPitch - this.pitch) * (1 - Math.exp(-delta / 180))
    const phase = (now - this.nodAt) / 720
    const nod = phase > 0 && phase < 1 ? .055 * Math.sin(Math.PI * smooth(phase)) ** 2 : 0
    this.nodPitch += (nod - this.nodPitch) * (1 - Math.exp(-delta / 90))
    return {
      active: this.amount,
      gaze: { yawDeg: this.yaw * this.amount, pitchDeg: this.pitch * this.amount },
      head: [(this.nodPitch - this.pitch * .0018) * this.amount, this.yaw * .003 * this.amount, -this.yaw * .001 * this.amount],
    }
  }
}
