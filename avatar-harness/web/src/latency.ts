export interface LatencyState { source?: 'voice' | 'text'; total_ms?: number; avatar_ms?: number; tts_ms?: number; llm_ms?: number }
type Metric = 'total_ms' | 'avatar_ms' | 'tts_ms' | 'llm_ms'

export class LatencyTracker {
  private turn: string | null = null
  private values: LatencyState = {}
  private changed: (values: LatencyState) => void
  constructor(changed: (values: LatencyState) => void) { this.changed = changed }
  begin(turn: string, source: 'voice' | 'text') { this.turn = turn; this.values = { source }; this.changed({ ...this.values }) }
  reset() { this.turn = null; this.values = {}; this.changed({}) }
  cancel() { this.turn = null } // Keep the last measured values visible.
  record(turn: string | null, metric: Metric, value: unknown) {
    if (!turn || turn !== this.turn || typeof value !== 'number' || !Number.isFinite(value) || value < 0 || this.values[metric] != null) return
    this.values[metric] = value; this.changed({ ...this.values })
  }
  timing(turn: string | null, data: Record<string, unknown>) {
    if (data.stage === 'llm.first_speech') this.record(turn, 'llm_ms', data.elapsed_ms)
    if (data.stage === 'tts.first_audio' && data.segment === 0) this.record(turn, 'tts_ms', data.elapsed_ms)
  }
}
