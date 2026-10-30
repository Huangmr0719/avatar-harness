export interface Transcript { input_id: string; text: string; final: boolean }
export interface LiveVoiceHooks {
  transcript: (event: Transcript) => void
  state: (state: string, message?: string) => void
}

// One local socket lives for the microphone session. The server owns cloud
// credentials and the cancellable recognition stream for each utterance.
export class RealtimeVoice {
  private socket?: WebSocket
  private generation = 0
  private inputID = ''
  private ready = false
  private opening?: () => void
  private pending = new Map<string, { resolve: () => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> }>()
  private hooks: LiveVoiceHooks

  constructor(hooks: LiveVoiceHooks) { this.hooks = hooks }
  get connected() { return this.ready && this.socket?.readyState === WebSocket.OPEN }

  connect(sessionID: string): Promise<void> {
    this.close()
    const generation = this.generation
    const url = new URL(`/api/sessions/${encodeURIComponent(sessionID)}/voice`, location.href)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(url.toString())
    this.socket = socket
    this.hooks.state('connecting')
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => fail('实时识别连接超时，请检查百炼配置。'), 12000)
      let settled = false
      this.opening = () => { clearTimeout(timer); if (!settled) { settled = true; reject(new DOMException('cancelled', 'AbortError')) } }
      const fail = (message: string) => {
        if (generation !== this.generation) return
        clearTimeout(timer)
        if (!settled) { settled = true; reject(new Error(message)) }
        this.close()
        this.hooks.state('error', message)
      }
      socket.onmessage = event => {
        if (generation !== this.generation) return
        let data: any
        try { data = JSON.parse(event.data) } catch { fail('识别服务返回无效事件。'); return }
        switch (data.type) {
          case 'ready':
            clearTimeout(timer); this.ready = true
            this.opening = undefined
            if (!settled) { settled = true; resolve() }
            this.hooks.state('ready'); break
          case 'transcript':
            if (data.input_id === this.inputID) this.hooks.transcript(data)
            break
          case 'cancelled': {
            const pending = this.pending.get(data.request_id)
            if (pending) { clearTimeout(pending.timer); this.pending.delete(data.request_id); pending.resolve() }
            break
          }
          case 'error': fail(data.message || '实时语音识别失败。'); break
        }
      }
      socket.onerror = () => fail('实时识别连接失败，请检查服务状态、地域与密钥。')
      socket.onclose = () => fail('实时识别连接已断开，请重新开始说话。')
    })
  }

  begin(inputID: string) {
    this.inputID = inputID
    this.send({ type: 'start', input_id: inputID })
    this.hooks.state('streaming')
  }
  audio(raw: Uint8Array) {
    if (!this.connected || !this.inputID) throw new Error('实时识别尚未连接。')
    if (this.socket!.bufferedAmount > 128000) { this.close(); throw new Error('网络积压，录音已停止，请重试。') }
    for (let offset = 0; offset < raw.length; offset += 3200) this.socket!.send(raw.slice(offset, offset + 3200))
  }
  commit(endedAt: number) {
    this.send({ type: 'commit', input_id: this.inputID, end_age_ms: Math.max(0, performance.now() - endedAt) })
    this.hooks.state('recognizing')
  }
  cancel(): Promise<void> {
    this.inputID = ''
    if (!this.connected) return Promise.resolve()
    const requestID = crypto.randomUUID()
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(requestID); this.close(); reject(new Error('停止识别未确认，已关闭连接。')) }, 2000)
      this.pending.set(requestID, { resolve, reject, timer })
      this.send({ type: 'cancel', request_id: requestID })
    })
  }
  private send(event: Record<string, unknown>) {
    if (!this.connected) throw new Error('实时识别尚未连接。')
    this.socket!.send(JSON.stringify(event))
  }
  close() {
    this.opening?.(); this.opening = undefined
    this.generation++; this.ready = false; this.inputID = ''
    if (this.socket) { this.socket.onmessage = null; this.socket.onclose = null; this.socket.onerror = null; this.socket.close(); this.socket = undefined }
    for (const item of this.pending.values()) { clearTimeout(item.timer); item.reject(new Error('识别连接已关闭。')) }
    this.pending.clear()
  }
}
