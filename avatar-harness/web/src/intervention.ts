export type SupportMode = 'mindfulness' | 'humanistic' | 'cbt'
export interface SupportParameters { duration_seconds: number; playback_rate: number; max_characters: number; question_depth: number; question_every: number; action_minutes: number }
export interface SupportExecution { id: string; turn_id?: string; mode: SupportMode; kind: 'video' | 'dialogue'; card_id: string; status: string; parameters: SupportParameters; presented: boolean; played_ms: number; feedback_recorded: boolean }
export interface SupportState { version: number; humanistic_override?: boolean; mode?: SupportMode; status?: string; goal?: string; stage?: string; parameters?: SupportParameters; dialogue_turns?: number; suggested_mode?: SupportMode; execution?: SupportExecution; action?: { id: string; text: string; minutes: number; status: string }; last_feedback?: string; last_decision?: { kind: string; reason: string } }
export interface SupportCommand { operation: string; enabled?: boolean; mode?: SupportMode; goal?: string; parameters?: SupportParameters; execution_id?: string; feedback?: string; event?: string; played_ms?: number }
export const supportNames: Record<SupportMode, string> = { mindfulness: '注意练习', humanistic: '倾听支持', cbt: '认知梳理' }
export function supportDefaults(mode: SupportMode): SupportParameters { return { duration_seconds: 60, playback_rate: 1, max_characters: mode === 'cbt' ? 200 : 160, question_depth: mode === 'cbt' ? 2 : 1, question_every: mode === 'cbt' ? 1 : 2, action_minutes: 2 } }
export function newerSupport(current: SupportState, incoming: SupportState): SupportState { return incoming.version >= current.version ? incoming : current }
export function canFeedback(s: SupportState): boolean { return !!s.execution?.presented && !s.execution.feedback_recorded && s.status !== 'stopped' }

// Count observed, advancing playback time. Waiting, paused, hidden tabs and
// large scheduling gaps do not count toward completion. Avatar affect is not an input.
export class PracticeClock {
  elapsed = 0
  private previous?: { now: number; media: number }
  update(now: number, media: number, playing: boolean, visible: boolean): number {
    const previous = this.previous
    if (previous && playing && visible && now - previous.now <= 500 && now >= previous.now && media > previous.media) this.elapsed += now - previous.now
    this.previous = playing && visible ? { now, media } : undefined
    return this.elapsed
  }
}
// A tool request is tied to one live turn, and waits for observed audio end.
export class PracticeLaunch {
  private request: { turn: string; execution: string } | null = null
  private controller: AbortController | null = null
  schedule(turn: string, execution: string) { this.cancel(); this.request = { turn, execution } }
  cancel() { this.request = null; this.controller?.abort(); this.controller = null }
  async finish(turn: string, wait: (signal: AbortSignal) => Promise<void>, current: (request: { turn: string; execution: string }) => boolean, play: () => Promise<void>) {
    const request = this.request
    if (!request || request.turn !== turn) return
    this.request = null
    const controller = new AbortController()
    this.controller = controller
    try {
      await wait(controller.signal)
      if (!controller.signal.aborted && current(request)) await play()
    } catch (error) {
      if (!controller.signal.aborted) throw error
    } finally { if (this.controller === controller) this.controller = null }
  }
}
