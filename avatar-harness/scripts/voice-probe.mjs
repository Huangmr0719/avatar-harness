// Real cloud probe using synthetic speech; never accesses a microphone/camera.
// Measures transport arrival, not browser playback or perceptual correctness.
import { writeFile } from 'node:fs/promises'
import { PCM16k, UtteranceVAD } from '../web/src/voice.ts'

const args = process.argv.slice(2)
const value = (name, fallback) => args.includes(name) ? args[args.indexOf(name) + 1] : fallback
const root = value('--url', 'http://127.0.0.1:8765')
const text = value('--text', '请用一句话解释什么是光合作用。')
const output = value('--output', '')
const interrupt = args.includes('--interrupt')
const wait = ms => new Promise(resolve => { const timer = setTimeout(resolve, ms); if (ms > 1000) timer.unref() })
const controller = new AbortController()
let socket, pending = '', speechEndedAt, firstPartialAt, finalAt, firstTextAt, firstAudioAt, turnID, answer = '', packets = 0, cancelSentAt, cancelAckAt, turnCancelled = false

async function api(path, body) {
  const res = await fetch(root + path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`)
  return res.json()
}

try {
  const state = await api('/api/state')
  if (!state.providers.asr.live_input || !state.providers.asr.configured || !state.providers.tts.configured || !state.providers.llm.configured) throw new Error('需要运行 0.3.0+ 并配置百炼 LLM/ASR/TTS。')
  const speech = await api('/v1/capabilities/tts/invoke', { input: { text } })
  if (speech.status !== 'ok') throw new Error(speech.error)
  const bytes = Buffer.from(speech.output.audio_base64, 'base64')
  const samples = new Float32Array(bytes.length / 2)
  for (let i = 0; i < samples.length; i++) samples[i] = bytes.readInt16LE(i * 2) / 32768
  const normalized = new PCM16k(speech.output.sample_rate).feed(samples)
  const view = new DataView(normalized.buffer)
  const mono = new Float32Array(normalized.length / 2 + 16000)
  for (let i = 0; i < normalized.length / 2; i++) mono[i] = view.getInt16(i * 2, true) / 32768

  const session = await api('/api/sessions', { character_id: state.characters[0].id })
  const inputID = crypto.randomUUID()
  let complete
  const completed = new Promise(resolve => { complete = resolve })
  const sse = await fetch(`${root}/api/sessions/${session.id}/events`, { signal: controller.signal })
  const listen = (async () => {
    const decoder = new TextDecoder()
    for await (const chunk of sse.body) {
      pending += decoder.decode(chunk, { stream: true })
      for (;;) {
        const end = pending.indexOf('\n\n')
        if (end < 0) break
        const block = pending.slice(0, end); pending = pending.slice(end + 2)
        const line = block.split('\n').find(item => item.startsWith('data: '))
        if (!line) continue
        const event = JSON.parse(line.slice(6))
        if (event.type === 'turn.started' && event.data.input_id === inputID) turnID = event.turn_id
        if (event.turn_id !== turnID || !turnID) continue
        if (event.type === 'assistant.delta') { firstTextAt ??= performance.now(); answer += event.data.text }
        if (event.type === 'pipeline.audio') {
          firstAudioAt ??= performance.now(); packets++
          if (interrupt && !cancelSentAt) { cancelSentAt = performance.now(); socket.send(JSON.stringify({ type: 'cancel', request_id: 'probe-cancel' })) }
        }
        if (event.type === 'turn.cancelled') { turnCancelled = true; if (interrupt && cancelAckAt) complete() }
        if (event.type === 'turn.error' || event.type === 'pipeline.error') throw new Error(event.data.message)
        if (event.type === 'turn.finished' && !interrupt) complete()
      }
    }
  })()
  void listen.catch(() => {})
  let socketError
  const ready = new Promise((resolve, reject) => {
    const url = new URL(`/api/sessions/${session.id}/voice`, root); url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    socket = new WebSocket(url)
    socket.onmessage = event => {
      const data = JSON.parse(event.data)
      if (data.type === 'ready') resolve()
      if (data.type === 'error') { socketError = new Error(data.message); reject(socketError); complete() }
      if (data.type === 'transcript' && data.input_id === inputID) {
        if (data.final) finalAt ??= performance.now()
        else firstPartialAt ??= performance.now()
      }
      if (data.type === 'cancelled' && data.request_id === 'probe-cancel') { cancelAckAt = performance.now(); if (turnCancelled) complete() }
    }
    socket.onerror = () => { socketError = new Error('本地实时语音连接失败'); reject(socketError); complete() }
  })
  await Promise.race([ready, wait(12000).then(() => { throw new Error('ASR 预热超时') })])
  const vad = new UtteranceVAD(() => socket.send(JSON.stringify({ type: 'start', input_id: inputID })), (_, __, endedAt) => {
    speechEndedAt = endedAt
    socket.send(JSON.stringify({ type: 'commit', input_id: inputID, end_age_ms: performance.now() - endedAt }))
  }, { silenceMS: state.limits.voice_silence_ms, onsetMS: state.limits.voice_onset_ms, audio: frame => {
    const pcm = new PCM16k(16000).feed(frame)
    socket.send(pcm)
  } })
  for (let offset = 0; offset < mono.length; offset += 512) {
    vad.feed(mono.subarray(offset, offset + 512), 16000, performance.now())
    await wait(32)
    if (socketError) throw socketError
  }
  await Promise.race([completed, listen, wait(30000).then(() => { throw new Error('语音轮次超时') })])
  if (socketError) throw socketError
  if (!firstAudioAt || !speechEndedAt) throw new Error('没有收到有效语音音频包')
  const elapsed = at => at == null ? null : Math.round((at - speechEndedAt) * 10) / 10
  const result = {
    time: new Date().toISOString(), source: 'synthetic_speech_paced_in_realtime', metric: 'speech_end_to_first_audio_packet_not_playback',
    input_text: text, model: state.providers.llm.model, session_id: session.id,
    partial_before_endpoint: firstPartialAt != null && firstPartialAt < speechEndedAt + state.limits.voice_silence_ms,
    partial_before_speech_end: firstPartialAt != null && firstPartialAt < speechEndedAt,
    first_partial_ms: elapsed(firstPartialAt), final_asr_ms: elapsed(finalAt), first_text_ms: elapsed(firstTextAt), first_audio_packet_ms: elapsed(firstAudioAt),
    target_ms: 3000, within_transport_target: firstAudioAt - speechEndedAt <= 3000, playback_verified: false, avatar_sync_verified: false,
    audio_packets_received: packets, answer, interrupted: interrupt ? turnCancelled : null, cancel_ack_ms: cancelAckAt ? Math.round((cancelAckAt - cancelSentAt) * 10) / 10 : null,
  }
  console.log(JSON.stringify(result, null, 2))
  if (output) await writeFile(output, JSON.stringify(result, null, 2) + '\n')
} finally {
  socket?.close(); controller.abort()
}
