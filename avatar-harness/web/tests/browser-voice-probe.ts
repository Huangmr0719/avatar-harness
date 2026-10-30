import { AudioPlayer } from '../src/audio'
import { RealtimeVoice } from '../src/realtimeVoice'
import { PCM16k, UtteranceVAD } from '../src/voice'

const button = document.querySelector<HTMLButtonElement>('button')!
const status = document.querySelector<HTMLElement>('#status')!
const output = document.querySelector<HTMLElement>('pre')!
const api = async (path: string, body?: unknown) => {
  const response = await fetch(path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  return response.json()
}
button.onclick = async () => {
  button.disabled = true; output.textContent = ''; status.textContent = '准备百炼合成录音，不访问麦克风或摄像头'
  let endedAt = 0, committedAt = 0, finalAt = 0, firstTextAt = 0, ttsAt = 0, firstPacketAt = 0, partialAt = 0, transcript = '', answer = '', sessionID = '', turnID = '', model = '', firstTextCommitted = false
  const inputID = crypto.randomUUID()
  let success: () => void, fail: (error: unknown) => void
  const done = new Promise<void>((resolve, reject) => { success = resolve; fail = reject })
  let reported = false
  const player = new AudioPlayer(() => {}, () => {}, async () => {
    if (reported || !endedAt) return
    reported = true
    try {
      const elapsed = performance.now() - endedAt
      const report = await api(`/api/sessions/${sessionID}/playback`, { turn_id: turnID, elapsed_ms: elapsed, modality: 'audio', meaningful: true, playback_observed: true })
      output.textContent = JSON.stringify({ source: 'browser_synthetic_speech', model, metric: 'speech_end_to_voiced_audio_context_playback',
        partial_before_speech_end: partialAt > 0 && partialAt < endedAt, transcript, answer_prefix: answer,
        first_audio_packet_ms: Math.round(firstPacketAt - endedAt), audio_context_playback_ms: Math.round(elapsed),
        endpoint_wait_ms: Math.round(committedAt - endedAt), asr_after_commit_ms: Math.round(finalAt - committedAt),
        first_text_after_asr_ms: Math.round(firstTextAt - finalAt), first_text_committed: firstTextCommitted,
        tts_queue_after_text_ms: Math.round(ttsAt - firstTextAt), tts_first_packet_ms: Math.round(firstPacketAt - ttsAt),
        audio_within_budget: report.audio_within_budget, physical_microphone_used: false, avatar_sync_verified: false,
      }, null, 2)
      status.textContent = report.audio_within_budget ? '浏览器音频起播在 3 秒内' : '本次浏览器起播超过 3 秒'
      success()
    } catch (error) { fail(error) }
  })
  const live = new RealtimeVoice({ transcript: event => { if (event.final) { transcript = event.text; finalAt = performance.now() } else { partialAt ||= performance.now(); status.textContent = `边说边识别：${event.text}` } }, state: (state, message) => { if (state === 'error') fail(new Error(message)) } })
  let events: EventSource | undefined
  const timer = setTimeout(() => fail(new Error('检查超时')), 30000)
  try {
    await player.unlock()
    const state = await api('/api/state')
    model = state.providers.llm.model
    const question = document.querySelector<HTMLInputElement>('input')!.value
    const speech = await api('/v1/capabilities/tts/invoke', { input: { text: question } })
    if (speech.status !== 'ok') throw new Error(speech.error)
    const bytes = Uint8Array.from(atob(speech.output.audio_base64), value => value.charCodeAt(0)), samples = new Float32Array(bytes.length / 2), view = new DataView(bytes.buffer)
    for (let i = 0; i < samples.length; i++) samples[i] = view.getInt16(i * 2, true) / 32768
    const pcm = new PCM16k(speech.output.sample_rate).feed(samples), normalized = new DataView(pcm.buffer), audio = new Float32Array(pcm.length / 2 + 16000)
    for (let i = 0; i < pcm.length / 2; i++) audio[i] = normalized.getInt16(i * 2, true) / 32768
    const session = await api('/api/sessions', { character_id: state.characters[0].id }); sessionID = session.id
    events = new EventSource(`/api/sessions/${sessionID}/events`)
    events.onmessage = event => {
      const item = JSON.parse(event.data)
      if (item.type === 'turn.started' && item.data.input_id === inputID) turnID = item.turn_id
      if (!turnID || item.turn_id !== turnID) return
      if (item.type === 'assistant.delta') { if (!firstTextAt) { firstTextAt = performance.now(); firstTextCommitted = !item.data.draft } answer += item.data.text }
      if (item.type === 'module.status' && item.data.module === 'tts' && item.data.state === 'running') ttsAt ||= performance.now()
      if (item.type === 'pipeline.audio') { firstPacketAt ||= performance.now(); void player.play(item.data).catch(fail) }
      if (item.type === 'turn.error' || item.type === 'pipeline.error') fail(new Error(item.data.message))
    }
    await live.connect(sessionID)
    const vad = new UtteranceVAD(() => live.begin(inputID), (_, __, end) => { endedAt = end; committedAt = performance.now(); live.commit(end); status.textContent = '说完，等待有效回复起播' },
      { silenceMS: state.limits.voice_silence_ms, onsetMS: state.limits.voice_onset_ms, audio: frame => live.audio(new PCM16k(16000).feed(frame)) })
    for (let offset = 0; offset < audio.length; offset += 512) { vad.feed(audio.subarray(offset, offset + 512), 16000, performance.now()); await new Promise(resolve => setTimeout(resolve, 32)) }
    await done
  } catch (error) { status.textContent = `检查失败：${String(error)}` }
  finally { clearTimeout(timer); live.close(); events?.close(); player.stop(); button.disabled = false }
}
