import { BASIC_EMOTIONS } from './expressionControl.ts'
import type { AudioPacket } from './lamSpeech.ts'

export const DEMO_TEXT = '这件事我已经知道了，我们现在就开始吧。'
export const EMOTION_LABELS: Record<string, string> = { neutral: '中性', happy: '快乐', sad: '悲伤', angry: '愤怒', fearful: '恐惧', surprised: '惊讶', disgusted: '厌恶' }
export const EMOTION_DEMO = BASIC_EMOTIONS.map(style => ({ style, label: EMOTION_LABELS[style]!, text: DEMO_TEXT, intensity: 1 as const }))
export type DemoStep = typeof EMOTION_DEMO[number]
export interface DemoState { phase: 'idle' | 'preparing' | 'playing' | 'holding' | 'complete' | 'cancelled' | 'error'; index: number; message?: string }
export const demoRunning = (state: DemoState) => ['preparing', 'playing', 'holding'].includes(state.phase)
interface DemoHooks {
  changed: (state: DemoState) => void
  synthesize: (step: DemoStep, signal: AbortSignal) => Promise<AudioPacket>
  play: (packet: AudioPacket, step: DemoStep, signal: AbortSignal) => Promise<void>
  reset: () => void
  finish: () => void
  hold?: (signal: AbortSignal) => Promise<void>
}

export function demoPause(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(new DOMException('演示已停止', 'AbortError')); return }
    const abort = () => { clearTimeout(timer); reject(new DOMException('演示已停止', 'AbortError')) }
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve() }, ms)
    signal.addEventListener('abort', abort, { once: true })
  })
}

// The fixed script uses configured TTS, without asking an LLM to choose or
// dilute the requested category/intensity.
export class EmotionDemo {
  private controller?: AbortController
  private hooks: DemoHooks
  constructor(hooks: DemoHooks) { this.hooks = hooks }
  cancel() {
    if (!this.controller) return
    const controller = this.controller
    this.controller = undefined
    controller.abort()
    this.hooks.finish()
    this.hooks.changed({ phase: 'cancelled', index: -1 })
  }
  async start() {
    this.cancel()
    const controller = new AbortController(), { signal } = controller
    this.controller = controller
    const current = () => this.controller === controller && !signal.aborted
    let index = 0
    try {
      for (const step of EMOTION_DEMO) {
        if (!current()) return
        this.hooks.reset()
        this.hooks.changed({ phase: 'preparing', index })
        const packet = await this.hooks.synthesize(step, signal)
        if (!current()) return
        this.hooks.changed({ phase: 'playing', index })
        await this.hooks.play(packet, step, signal)
        if (!current()) return
        this.hooks.changed({ phase: 'holding', index })
        await (this.hooks.hold?.(signal) ?? demoPause(1100, signal))
        index++
      }
      if (current()) this.hooks.changed({ phase: 'complete', index: EMOTION_DEMO.length - 1 })
    } catch (error) {
      if (current()) this.hooks.changed({ phase: 'error', index, message: String(error) })
    } finally {
      if (this.controller === controller) { this.controller = undefined; this.hooks.finish() }
    }
  }
}
