import assert from 'node:assert/strict'
import test from 'node:test'
import { PCM16k, UtteranceVAD } from '../src/voice.ts'
import { RealtimeVoice } from '../src/realtimeVoice.ts'

test('continuous PCM resampling preserves samples across packet boundaries', () => {
  const samples = Float32Array.from({ length: 4800 }, (_, index) => Math.sin(index / 20) * .2)
  const all = new PCM16k(48000).feed(samples)
  const stream = new PCM16k(48000), chunks = []
  for (let index = 0; index < samples.length; index += 137) chunks.push(Buffer.from(stream.feed(samples.subarray(index, index + 137))))
  assert.deepEqual(Buffer.concat(chunks), Buffer.from(all))
  assert.equal(all.length, 3200)
})

test('live VAD uploads audio before the endpoint and ignores a short noise spike', () => {
  const sent = [], starts = [], commits = []
  const vad = new UtteranceVAD(() => starts.push(true), (_, __, endedAt) => commits.push(endedAt), { silenceMS: 400, onsetMS: 80, audio: samples => sent.push(samples) })
  const quiet = new Float32Array(320), speech = new Float32Array(320).fill(.1)
  vad.feed(speech, 16000, 20)
  vad.feed(quiet, 16000, 40)
  assert.equal(starts.length, 0)
  for (let now = 100; now <= 400; now += 20) vad.feed(speech, 16000, now)
  assert.equal(starts.length, 1)
  assert.ok(sent.length > 0)
  assert.equal(commits.length, 0)
  for (let now = 420; now < 800; now += 20) vad.feed(quiet, 16000, now)
  assert.equal(commits.length, 0)
  vad.feed(quiet, 16000, 800)
  assert.deepEqual(commits, [400])
})

function socketFixture() {
  const sockets = []
  globalThis.location = new URL('http://127.0.0.1:8765/')
  globalThis.WebSocket = class {
    static OPEN = 1
    readyState = 1
    bufferedAmount = 0
    sent = []
    constructor(url) { this.url = url; sockets.push(this) }
    send(value) { this.sent.push(value) }
    close() { this.readyState = 3 }
    event(value) { this.onmessage?.({ data: JSON.stringify(value) }) }
  }
  return sockets
}

test('voice cancellation is acknowledged and stale partials cannot cross utterances', async () => {
  const sockets = socketFixture(), transcripts = []
  const client = new RealtimeVoice({ state() {}, transcript: event => transcripts.push(event) })
  const opened = client.connect('s1')
  sockets[0].event({ type: 'ready' }); await opened
  client.begin('first')
  client.audio(new Uint8Array(640))
  sockets[0].event({ type: 'transcript', input_id: 'first', text: '第一句', final: false })
  const stopping = client.cancel()
  const request = JSON.parse(sockets[0].sent.at(-1))
  sockets[0].event({ type: 'transcript', input_id: 'first', text: '迟到的最终结果', final: true })
  sockets[0].event({ type: 'cancelled', request_id: request.request_id }); await stopping
  client.begin('second')
  sockets[0].event({ type: 'transcript', input_id: 'first', text: '旧结果', final: true })
  sockets[0].event({ type: 'transcript', input_id: 'second', text: '第二句', final: false })
  assert.deepEqual(transcripts.map(item => item.text), ['第一句', '第二句'])
  assert.ok(sockets[0].sent.some(item => item instanceof Uint8Array))
  client.close()
})

test('cancelling microphone connection before readiness rejects the pending startup', async () => {
  socketFixture()
  const client = new RealtimeVoice({ state() {}, transcript() {} })
  const opened = client.connect('s1')
  client.close()
  await assert.rejects(opened, { name: 'AbortError' })
})
