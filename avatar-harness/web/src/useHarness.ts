import { computed, onUnmounted, ref, shallowRef } from 'vue'
import { AudioPlayer } from './audio'
import { SpeechBatcher } from './lamSpeech'
import { VoiceInput, deviceError } from './voice'
import { RealtimeVoice } from './realtimeVoice'
import { ExpressionControl, type ExpressionState } from './expressionControl'
import { LatencyTracker, type LatencyState } from './latency'
import type { AudioSegment, Entry, HarnessEvent, MotionFrame, Renderer, Session, State } from './types'

export async function api<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...options?.headers } })
  if (!response.ok) { const error = await response.json().catch(() => ({})); throw new Error(typeof error.detail === 'string' ? error.detail : `HTTP ${response.status}`) }
  return response.json()
}
export interface MediaHooks { prepare?: (buffer: AudioBuffer, pts: number) => Promise<{ elapsed_ms: number } | undefined>; frames: (frames: MotionFrame[], base: number) => void; audio: (segment: AudioSegment) => void; probe: () => { at_ms: number; audio_pts_ms: number; frame_pts_ms: number } | null }

export function useHarness() {
  const state = ref<State | null>(null), sessions = ref<Session[]>([]), selected = ref<Session | null>(null)
  const entries = ref<Entry[]>([]), connected = ref(false), busy = ref(false), playing = ref(false), error = ref('')
  const duration = ref<number | null>(null), activeTurn = ref<string | null>(null)
  const moduleStates = ref<Record<string, { state: string; message?: string }>>({})
  const renderer = shallowRef<Renderer>({ kind: 'none' }), renderState = ref('none'), renderError = ref('')
  const cameraStream = shallowRef<MediaStream | null>(null), cameraState = ref('off'), micState = ref('off'), micLevel = ref(0)
  const feedback = ref<{ audio_ms?: number; av_ms?: number; origin?: string }>({})
  const liveTranscript = ref(''), voiceConnectionState = ref('off')
  const character = computed(() => state.value?.characters.find(item => item.id === selected.value?.character_id))
  const mediaHooks = shallowRef<MediaHooks | null>(null)
  const expressionState = ref<ExpressionState>({ mode: 'auto', style: 'neutral', intensity: .7, source: 'default' })
  const expressionControl = new ExpressionControl(value => { expressionState.value = value })
  const latency = ref<LatencyState>({})
  const latencyTracker = new LatencyTracker(value => { latency.value = value })
  let stream: EventSource | null = null, lastSeq = 0, selectionGeneration = 0, cameraGeneration = 0, micGeneration = 0
  let pendingOrigin = performance.now(), pendingVoice = 0, turnOrigin = 0, turnSource = 'text', reported = false
  let asrAbort: AbortController | null = null
  let voiceInputID = '', speechPending = false
  const voiceOrigins = new Map<string, number>(), blockedTurns = new Set<string>()
  const timers = new Set<ReturnType<typeof setTimeout>>()

  function notice(text: string, status?: string) { entries.value.push({ id: crypto.randomUUID(), at: Date.now() / 1000, kind: 'notice', text, status }) }
  const audio = new AudioPlayer(value => { playing.value = value }, segment => {
    mediaHooks.value?.audio(segment)
    const tid = activeTurn.value, session = selected.value
    if (!tid || !session) return
    const verify = () => {
      if (activeTurn.value !== tid || reported) return
      const probe = mediaHooks.value?.probe(), now = performance.now()
      if (feedback.value.audio_ms != null && probe && renderState.value === 'ready' && now - probe.at_ms < 50 && Math.abs(probe.frame_pts_ms - probe.audio_pts_ms) <= 100) {
        reported = true
        const elapsed = Math.max(0, probe.at_ms - turnOrigin)
        feedback.value = { ...feedback.value, av_ms: elapsed }
        void api(`/api/sessions/${session.id}/playback`, { method: 'POST', body: JSON.stringify({ turn_id: tid, elapsed_ms: elapsed, modality: 'audio_video', meaningful: true, av_skew_ms: probe.frame_pts_ms - probe.audio_pts_ms }) }).catch(exc => notice(String(exc), 'error'))
      } else if (now < segment.start_ms + 500) {
        const timer = setTimeout(() => { timers.delete(timer); verify() }, 20); timers.add(timer)
      }
    }
    const timer = setTimeout(() => { timers.delete(timer); verify() }, Math.max(0, segment.start_ms - performance.now() + 30)); timers.add(timer)
  }, () => {
    const tid = activeTurn.value, session = selected.value
    if (!tid || !session || feedback.value.audio_ms != null) return
    const elapsed = Math.max(0, performance.now() - turnOrigin)
    feedback.value = { ...feedback.value, audio_ms: elapsed, origin: turnSource }
    latencyTracker.record(tid, 'total_ms', elapsed)
    void api(`/api/sessions/${session.id}/playback`, { method: 'POST', body: JSON.stringify({ turn_id: tid, elapsed_ms: elapsed, modality: 'audio', meaningful: true, playback_observed: true }) }).catch(exc => notice(String(exc), 'error'))
  }, async (buffer, pts) => {
    if (renderer.value.kind !== 'lam') return
    const turn = activeTurn.value
    const timing = await mediaHooks.value?.prepare?.(buffer, pts)
    if (turn === activeTurn.value && timing) latencyTracker.record(turn, 'avatar_ms', timing.elapsed_ms)
  })
  const speechBatcher = new SpeechBatcher(packet => { void audio.play(packet).catch(exc => notice(`本地语音驱动：${String(exc)}`, 'error')) })

  function cutPlayback() {
    latencyTracker.cancel()
    expressionControl.cancel()
    if (activeTurn.value) blockedTurns.add(activeTurn.value)
    if (blockedTurns.size > 256) blockedTurns.delete(blockedTurns.values().next().value!)
    speechBatcher.reset(); audio.stop(); activeTurn.value = null; busy.value = false; asrAbort?.abort()
    timers.forEach(timer => clearTimeout(timer)); timers.clear()
  }
  const liveVoice = new RealtimeVoice({
    transcript: event => {
      if (event.input_id !== voiceInputID) return
      liveTranscript.value = event.text
      moduleStates.value.asr = { state: event.final ? 'configured' : 'running' }
    },
    state: (value, message) => {
      voiceConnectionState.value = value
      if (value === 'error') {
        micGeneration++; micState.value = 'error'; speechPending = false; voiceInputID = ''; liveTranscript.value = ''
        moduleStates.value.asr = { state: 'error', message }; void voice.stop(); notice(message || '实时识别已断开。', 'error')
      } else moduleStates.value.asr = { state: value === 'streaming' || value === 'recognizing' || speechPending ? 'running' : 'configured' }
    },
  })

  function handle(event: HarnessEvent, replay = false) {
    lastSeq = Math.max(lastSeq, event.seq)
    const data = event.data, turnId = event.turn_id ?? undefined, at = event.at
    if (!replay && event.type === 'turn.started' && ((data.input_id && data.input_id !== voiceInputID) || (!data.input_id && speechPending))) {
      if (turnId) blockedTurns.add(turnId)
      return
    }
    if (!replay && turnId && blockedTurns.has(turnId)) return
    switch (event.type) {
      case 'turn.started':
        if (!replay && event.turn_id) expressionControl.begin(event.turn_id)
        if (!replay && event.turn_id) latencyTracker.begin(event.turn_id, data.source === 'voice' ? 'voice' : 'text')
        busy.value = !replay; activeTurn.value = event.turn_id; duration.value = null; feedback.value = {}; reported = false
        turnSource = data.source; turnOrigin = data.input_id ? voiceOrigins.get(data.input_id) ?? performance.now() : data.source === 'voice' && pendingVoice ? pendingVoice : pendingOrigin
        if (!replay && data.input_id) { speechPending = false; liveTranscript.value = '' }
        break
      case 'user.message': entries.value.push({ id: `user-${event.seq}`, at, kind: 'user', text: data.text, turn_id: turnId }); break
      case 'assistant.delta': {
        let entry = entries.value.find(item => item.id === `assistant-${turnId}`)
        if (!entry) { entry = { id: `assistant-${turnId}`, at, kind: 'assistant', text: '', draft: !!data.draft, turn_id: turnId }; entries.value.push(entry) }
        entry.text += data.text; break
      }
      case 'assistant.draft_reset': entries.value = entries.value.filter(item => item.id !== `assistant-${turnId}`); break
      case 'assistant.message': {
        const entry = entries.value.find(item => item.id === `assistant-${turnId}`)
        if (entry) Object.assign(entry, { text: data.text, draft: false, source: data.source })
        else entries.value.push({ id: `assistant-${turnId}`, at, kind: 'assistant', text: data.text, source: data.source, turn_id: turnId })
        break
      }
      case 'tool.started': entries.value.push({ id: `tool-${data.call_id}`, at, kind: 'tool', text: '', name: data.name, status: 'running', args: data.arguments, turn_id: turnId }); break
      case 'tool.finished': {
        let entry = entries.value.find(item => item.id === `tool-${data.call_id}`)
        if (!entry) { entry = { id: `tool-${data.call_id}`, at, kind: 'tool', text: '', name: data.name, turn_id: turnId }; entries.value.push(entry) }
        Object.assign(entry, { status: data.status, result: data.error ? { error: data.error, output: data.output } : data.output, elapsed_ms: data.elapsed_ms }); break
      }
      case 'module.status': if (!replay) moduleStates.value[data.module] = { state: data.state, message: data.message }; break
      case 'turn.error': if (!replay) cutPlayback(); entries.value.push({ id: `error-${event.seq}`, at, kind: 'notice', text: data.message, status: 'error' }); break
      case 'pipeline.error': entries.value.push({ id: `error-${event.seq}`, at, kind: 'notice', text: data.message, status: 'error' }); break
      case 'deadline.missed': if (!replay) notice('本轮有效语音尚未在 3 秒内起播。', 'timeout'); break
      case 'pipeline.avatar': if (!replay && activeTurn.value === event.turn_id && Array.isArray(data.frames)) mediaHooks.value?.frames(data.frames, data.pts_ms ?? 0); break
      case 'pipeline.expression': if (!replay && activeTurn.value === event.turn_id) expressionControl.apply(event.turn_id, data); break
      case 'pipeline.timing': if (!replay) latencyTracker.timing(event.turn_id, data); break
      case 'pipeline.audio': if (!replay && data.audio_base64 && activeTurn.value === event.turn_id) { if (renderer.value.kind === 'lam') speechBatcher.add(data as any); else void audio.play(data as any).catch(exc => notice(String(exc), 'error')) }; break
      case 'session.cleared': entries.value = []; duration.value = null; break
      case 'turn.cancelled': if (!event.turn_id || activeTurn.value === event.turn_id) { latencyTracker.cancel(); expressionControl.cancel(); speechBatcher.reset(); audio.stop(); busy.value = false; activeTurn.value = null }; break
      case 'turn.finished': if (!replay) speechBatcher.flush(); busy.value = false; duration.value = data.duration_ms; break
    }
    if (entries.value.length > 600) entries.value = entries.value.slice(-500)
  }
  async function resolveAvatar() {
    if (!selected.value) return
    const sid = selected.value.id
    try { const result = await api<{ renderer: Renderer }>(`/api/sessions/${sid}/avatar`, { method: 'POST', body: '{}' }); if (selected.value?.id === sid) setRenderer(result.renderer) }
    catch (exc) { if (selected.value?.id === sid) notice(`形象服务：${String(exc)}`, 'error') }
  }
  function setRenderer(value: Renderer) {
    const previous = renderer.value
    if (renderState.value !== 'error' && previous.kind === value.kind && previous.asset_url === value.asset_url && previous.url === value.url && previous.includes_audio === value.includes_audio) return
    renderer.value = { ...value }; renderState.value = value.kind === 'none' ? 'none' : 'loading'; renderError.value = ''
  }
  function setRenderStatus(value: string, message = '') { renderState.value = value; renderError.value = message; if (value === 'error') notice(`形象显示失败：${message}`, 'error') }
  async function select(session: Session) {
    latencyTracker.reset()
    expressionControl.cancel()
    const generation = ++selectionGeneration
    if (selected.value) await stop()
    liveVoice.close(); micGeneration++; await voice.stop(); micState.value = 'off'; voiceConnectionState.value = 'off'; liveTranscript.value = ''
    stream?.close(); asrAbort?.abort(); speechBatcher.reset(); audio.stop(); selected.value = session; entries.value = []; lastSeq = 0
    duration.value = null; busy.value = false; connected.value = false; activeTurn.value = null; moduleStates.value = {}; feedback.value = {}
    setRenderer(character.value?.renderer ?? { kind: 'none' })
    const history = await api<HarnessEvent[]>(`/api/sessions/${session.id}/history`)
    if (generation !== selectionGeneration) return
    history.forEach(event => handle(event, true)); busy.value = false; activeTurn.value = null
    stream = new EventSource(`/api/sessions/${session.id}/events?after=${lastSeq}`)
    stream.onopen = () => { connected.value = true; error.value = '' }
    stream.onerror = () => { connected.value = false }
    stream.onmessage = event => handle(JSON.parse(event.data))
    stream.addEventListener('resync', () => { speechBatcher.reset(); audio.stop(); activeTurn.value = null; notice('接收积压，已停止旧音频。请重新发起本轮。', 'error'); void stop() })
    localStorage.setItem('avatar-harness-session', session.id)
    if (state.value?.providers.avatar.configured || state.value?.oac_url) void resolveAvatar()
  }
  async function newSession(characterId = character.value?.id) {
    if (busy.value || playing.value) await stop()
    const session = await api<Session>('/api/sessions', { method: 'POST', body: JSON.stringify({ character_id: characterId }) })
    sessions.value.unshift(session); await select(session)
  }
  async function init() {
    try {
      state.value = await api<State>('/api/state'); sessions.value = await api<Session[]>('/api/sessions')
      const remembered = localStorage.getItem('avatar-harness-session')
      const previous = sessions.value.find(item => item.id === remembered) ?? sessions.value[0]
      if (previous) await select(previous); else await newSession(state.value.characters[0]?.id)
      if (!entries.value.length) notice('本地工具已就绪。')
    } catch (exc) { error.value = String(exc) }
  }
  async function send(text: string) {
    if (!selected.value || !text.trim()) return
    error.value = ''; pendingOrigin = performance.now()
    try { await stop(); if (state.value?.providers.tts.configured) await audio.unlock(); await api(`/api/sessions/${selected.value.id}/turns`, { method: 'POST', body: JSON.stringify({ text: text.trim() }) }) }
    catch (exc) { error.value = String(exc); busy.value = false }
  }
  async function stop() {
    cutPlayback(); voiceInputID = ''; speechPending = false; liveTranscript.value = ''; voice.discard()
    for (const [name, status] of Object.entries(moduleStates.value)) if (status.state === 'running') moduleStates.value[name] = { state: state.value?.providers[name]?.configured ? 'configured' : 'deferred' }
    timers.forEach(timer => clearTimeout(timer)); timers.clear()
    if (liveVoice.connected) await liveVoice.cancel()
    else if (selected.value) await api(`/api/sessions/${selected.value.id}/stop`, { method: 'POST' })
    busy.value = false
  }
  async function clear() {
    await stop()
    if (selected.value) await api(`/api/sessions/${selected.value.id}/messages`, { method: 'DELETE' })
    entries.value = []; duration.value = null
  }
  async function toggleCamera() {
    const generation = ++cameraGeneration
    if (cameraState.value === 'on' || cameraState.value === 'requesting') { cameraStream.value?.getTracks().forEach(track => track.stop()); cameraStream.value = null; cameraState.value = 'off'; return }
    cameraState.value = 'requesting'
    try {
      if (!navigator.mediaDevices?.getUserMedia) throw new Error('请使用支持设备访问的 localhost 或 HTTPS 浏览器。')
      const value = await navigator.mediaDevices.getUserMedia({ video: { width: { ideal: 640 }, height: { ideal: 360 }, facingMode: 'user' }, audio: false })
      if (generation !== cameraGeneration) { value.getTracks().forEach(track => track.stop()); return }
      cameraStream.value = value; cameraState.value = 'on'; notice('摄像头已开启，仅在本地显示，没有上传画面。')
    } catch (exc) { if (generation === cameraGeneration) { cameraState.value = 'error'; notice(deviceError(exc, '摄像头'), 'error') } }
  }
  const voice = new VoiceInput(value => { micLevel.value = value }, () => {
    cutPlayback()
    if (liveVoice.connected) {
      voiceInputID = crypto.randomUUID(); speechPending = true; liveTranscript.value = ''; liveVoice.begin(voiceInputID)
    } else if (selected.value) void api(`/api/sessions/${selected.value.id}/stop`, { method: 'POST' }).catch(exc => notice(String(exc), 'error'))
  }, (input, endedAt) => { void submitVoice(input, endedAt) })
  async function submitVoice(input: Record<string, unknown>, endedAt: number) {
    if (!selected.value) return
    if (!state.value?.providers.asr.configured) { notice('检测到语音；ASR 尚未配置，音频未上传。请在终端运行 ./start configure asr。'); return }
    const session = selected.value.id
    asrAbort?.abort(); asrAbort = new AbortController(); pendingVoice = endedAt
    try { if (state.value?.providers.tts.configured) await audio.unlock(); await api(`/api/sessions/${session}/audio`, { method: 'POST', signal: asrAbort.signal, body: JSON.stringify({ input }) }) }
    catch (exc) { if ((exc as Error).name !== 'AbortError') notice(`语音识别失败：${String(exc)}`, 'error') }
  }
  async function toggleMic() {
    const generation = ++micGeneration
    if (micState.value === 'on' || micState.value === 'requesting') { liveVoice.close(); voiceInputID = ''; speechPending = false; liveTranscript.value = ''; await voice.stop(); micState.value = 'off'; voiceConnectionState.value = 'off'; return }
    micState.value = 'requesting'
    try {
      await audio.unlock()
      const realtime = !!state.value?.providers.asr.configured && !!state.value?.providers.asr.live_input
      if (realtime && selected.value) await liveVoice.connect(selected.value.id)
      if (generation !== micGeneration) return
      await voice.start({ silenceMS: state.value?.limits.voice_silence_ms ?? 400, onsetMS: state.value?.limits.voice_onset_ms ?? 80,
        onError: exc => { liveVoice.close(); micState.value = 'error'; speechPending = false; notice(String(exc), 'error') },
        sink: realtime ? {
          audio: raw => liveVoice.audio(raw),
          commit: endedAt => { voiceOrigins.set(voiceInputID, endedAt); pendingVoice = endedAt; if (voiceOrigins.size > 64) voiceOrigins.delete(voiceOrigins.keys().next().value!); liveVoice.commit(endedAt) },
          discard: () => { voiceInputID = ''; speechPending = false; void liveVoice.cancel().catch(exc => notice(String(exc), 'error')) },
        } : undefined,
      })
      if (generation === micGeneration) { micState.value = 'on'; notice(realtime ? '实时识别已连接，可边说边识别；再次开口会打断当前回应。' : state.value?.providers.asr.configured ? '麦克风已开启，使用整句识别接口。' : '麦克风仅做本地检测，ASR 尚未接入。') }
    }
    catch (exc) { liveVoice.close(); if (generation === micGeneration && (exc as Error).name !== 'AbortError') { micState.value = 'error'; notice(deviceError(exc, '麦克风'), 'error') } }
  }
  onUnmounted(() => { latencyTracker.cancel(); expressionControl.cancel(); speechBatcher.reset(); liveVoice.close(); stream?.close(); asrAbort?.abort(); audio.stop(); cameraGeneration++; micGeneration++; cameraStream.value?.getTracks().forEach(track => track.stop()); void voice.stop(); timers.forEach(timer => clearTimeout(timer)) })
  return { latency, expressionState, setExpressionMode: (mode: string) => expressionControl.setMode(mode), state, sessions, selected, entries, connected, busy, playing, error, duration, activeTurn, character, moduleStates, renderer, renderState, renderError, cameraStream, cameraState, micState, micLevel, liveTranscript, voiceConnectionState, feedback, mediaHooks, init, select, newSession, send, stop, clear, toggleCamera, toggleMic, resolveAvatar, setRenderer, setRenderStatus, notice }
}
