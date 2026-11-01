const smooth = (value: number) => value * value * (3 - 2 * value)

// Schedule from the avatar's own clock, rather than blinking on a global loop.
export class Blink {
  private next?: number
  private started?: number
  private lastTime?: number
  private observedClosed = false
  private random: () => number
  constructor(random: () => number = Math.random) { this.random = random }
  private interval() { return 2600 + this.random() * 4000 }
  sample(now: number, observedClosure = 0): number {
    // Returning from a background tab must not replay a missed blink.
    if (this.next == null || this.lastTime != null && now - this.lastTime > 1000) {
      this.started = undefined
      this.next = now + this.interval()
    }
    this.lastTime = now
    const closed = observedClosure > .55
    if (closed && !this.observedClosed) {
      this.started = undefined
      this.next = now + this.interval()
    }
    this.observedClosed = closed
    if (closed) return 0 // Audio-driven closure already owns this blink.
    if (this.started == null) {
      if (now < this.next) return 0
      this.started = now
    }
    const elapsed = now - this.started
    if (elapsed < 65) return smooth(elapsed / 65)
    if (elapsed < 100) return 1
    if (elapsed < 240) return 1 - smooth((elapsed - 100) / 140)
    this.started = undefined
    this.next = now + this.interval()
    return 0
  }
}
