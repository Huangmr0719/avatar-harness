export interface FaceObservation {
  face_detected: boolean
  face_count: number
  quality: string
  au_intensities?: Record<string, number>
  au_detection?: Record<string, { present: boolean; probability: number }>
  expression?: { label: string; score: number; score_type: string; scores: Record<string, number> } | null
  inference_ms?: number
}

// One in-flight frame; a server-issued token prevents late frames from reviving
// analysis after it is stopped or a different conversation is selected.
export class FaceAnalysis {
  private generation = 0
  private abort?: AbortController
  private timer?: ReturnType<typeof setTimeout>
  private token = ''
  private url = ''
  private report: (state: string, observation?: FaceObservation, error?: string) => void
  constructor(report: (state: string, observation?: FaceObservation, error?: string) => void) { this.report = report }
  async start(session: string, capture: () => Promise<string | null>) {
    this.stop()
    const generation = this.generation
    const url = `/api/sessions/${encodeURIComponent(session)}/perception`
    this.url = url
    this.abort = new AbortController()
    this.report('connecting')
    try {
      const { token } = await this.request(url, { operation: 'start' }, this.abort.signal)
      if (generation !== this.generation) { void this.release(url, token); return }
      this.token = token
      let frame = 0
      const sample = async () => {
        const started = performance.now()
        try {
          if (document.visibilityState === 'visible') {
            const image = await capture()
            if (generation !== this.generation) return
            if (image) {
              const result = await this.request(url, { operation: 'frame', token, frame: ++frame, age_ms: performance.now() - started, image_base64: image }, this.abort?.signal)
              if (generation !== this.generation) return
              this.report('on', result)
            }
          }
          if (generation === this.generation) this.timer = setTimeout(sample, Math.max(30, 1000 - (performance.now() - started)))
        } catch (error) {
          if (generation !== this.generation) return
          this.stop(); this.report('error', undefined, String(error))
        }
      }
      void sample()
    } catch (error) { if (generation === this.generation) { this.stop(); this.report('error', undefined, String(error)) } }
  }
  stop() {
    this.generation++; clearTimeout(this.timer); this.abort?.abort()
    if (this.token) void this.release(this.url, this.token)
    this.token = ''; this.report('off')
  }
  private async release(url: string, token: string) {
    await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ operation: 'stop', token }), keepalive: true }).catch(() => {})
  }
  private async request(url: string, body: unknown, signal?: AbortSignal) {
    const result = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal })
    const data = await result.json()
    if (!result.ok) throw new Error(data.detail || '表情分析连接失败')
    return data
  }
}
