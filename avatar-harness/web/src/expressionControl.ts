export const BASIC_EMOTIONS = ['neutral', 'happy', 'sad', 'angry', 'fearful', 'surprised', 'disgusted'] as const
export type ExpressionStyle = typeof BASIC_EMOTIONS[number] | 'natural' | 'focused' | 'concerned'
export type ExpressionMode = 'auto' | ExpressionStyle
export interface ExpressionState { mode: ExpressionMode; style: ExpressionStyle; intensity: number; source: 'llm' | 'manual' | 'default' }
const styles = new Set<string>([...BASIC_EMOTIONS, 'natural', 'focused', 'concerned'])

// This lane never prepares audio. It only updates render state, and rejects
// replayed, cancelled and previous-turn decisions before touching the face.
export class ExpressionControl {
  private mode: ExpressionMode = 'auto'
  private turn: string | null = null
  private automatic: ExpressionState = { mode: 'auto', style: 'neutral', intensity: .7, source: 'default' }
  private timer?: ReturnType<typeof setTimeout>
  private releaseAt = 0
  private changed: (state: ExpressionState) => void
  constructor(changed: (state: ExpressionState) => void) { this.changed = changed }
  get state(): ExpressionState {
    return this.mode === 'auto' ? { ...this.automatic } : { mode: this.mode, style: this.mode, intensity: 1, source: 'manual' }
  }
  begin(turn: string) { this.cancel(); this.turn = turn }
  cancel() {
    this.turn = null; clearTimeout(this.timer); this.releaseAt = 0
    this.automatic = { mode: 'auto', style: 'neutral', intensity: .7, source: 'default' }
    this.changed(this.state)
  }
  setMode(mode: string) {
    if (mode !== 'auto' && !styles.has(mode)) return
    this.mode = mode as ExpressionMode; this.changed(this.state)
  }
  apply(turn: string | null, data: Record<string, unknown>): boolean {
    if (!turn || this.turn !== turn || !styles.has(String(data.style))) return false
    const intensity = data.intensity ?? .7, duration = data.duration_ms ?? 8000
    if (typeof intensity !== 'number' || !Number.isFinite(intensity) || intensity < 0 || intensity > 1 || typeof duration !== 'number' || !Number.isFinite(duration) || duration < 500 || duration > 15000) return false
    clearTimeout(this.timer)
    this.automatic = { mode: 'auto', style: data.style as ExpressionStyle, intensity, source: 'llm' }
    this.changed(this.state)
    this.releaseAt = Math.max(this.releaseAt, performance.now() + duration)
    this.scheduleRelease(turn)
    return true
  }
  // The original fixed TTL could return to a blank face while audio still ran.
  holdThrough(turn: string, playbackEnd: number): boolean {
    if (this.turn !== turn || this.automatic.source !== 'llm' || !Number.isFinite(playbackEnd)) return false
    this.releaseAt = Math.max(this.releaseAt, playbackEnd + 250)
    this.scheduleRelease(turn)
    return true
  }
  private scheduleRelease(turn: string) {
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { if (this.turn === turn) { this.releaseAt = 0; this.automatic = { mode: 'auto', style: 'neutral', intensity: .7, source: 'default' }; this.changed(this.state) } }, Math.max(0, this.releaseAt - performance.now()))
    if (typeof this.timer === 'object' && 'unref' in this.timer) (this.timer as { unref: () => void }).unref()
  }
}
