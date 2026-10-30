import assert from 'node:assert/strict'
import test from 'node:test'
import { AvatarTimeline } from '../src/avatarTimeline.ts'
import { UtteranceVAD, wav, VoiceInput, deviceError } from '../src/voice.ts'

test('avatar timeline follows actual audio segments and freezes across network gaps', () => {
  const timeline = new AvatarTimeline()
  timeline.addFrames([{ offset_ms: 0, blendshapes: { jawOpen: 0 } }, { offset_ms: 40, blendshapes: { jawOpen: .8 } }])
  timeline.addFrames([{ offset_ms: 0, blendshapes: { jawOpen: .2 } }], 200)
  timeline.addAudio({ pts_ms: 0, start_ms: 1000, duration_ms: 100 })
  timeline.addAudio({ pts_ms: 200, start_ms: 1400, duration_ms: 100 })
  assert.equal(timeline.sample(999), null)
  assert.equal(timeline.sample(1050).frame.blendshapes.jawOpen, .8)
  assert.equal(timeline.sample(1350).audio_pts_ms, 100)
  assert.equal(timeline.sample(1420).frame.blendshapes.jawOpen, .2)
  timeline.reset()
  assert.equal(timeline.sample(1450), null)
})
test('microphone encoding emits real mono PCM WAV with clipping and original sample rate', () => {
  const raw = Buffer.from(wav(new Float32Array([-2, 0, .5, 2]), 48000), 'base64')
  assert.equal(raw.toString('ascii', 0, 4), 'RIFF')
  assert.equal(raw.readUInt32LE(24), 48000)
  assert.equal(raw.readUInt32LE(40), 8)
  assert.equal(raw.readInt16LE(44), -32768)
  assert.equal(raw.readInt16LE(50), 32767)
})
test('voice endpoint occurs after silence and retains the actual speech end timestamp', () => {
  const result = [], starts = []
  const vad = new UtteranceVAD(() => starts.push(true), (...args) => result.push(args))
  const quiet = new Float32Array(320), voice = new Float32Array(320).fill(.2)
  for (let now = 0; now < 200; now += 20) vad.feed(quiet, 16000, now)
  for (let now = 200; now <= 400; now += 20) vad.feed(voice, 16000, now)
  for (let now = 420; now < 1000; now += 20) vad.feed(quiet, 16000, now)
  assert.equal(result.length, 0)
  vad.feed(quiet, 16000, 1000)
  assert.equal(starts.length, 1)
  assert.equal(result.length, 1)
  assert.equal(result[0][1], 16000)
  assert.equal(result[0][2], 400)
  assert.ok(result[0][0].length > 3200)
})
test('denied microphone permission returns an actionable message and never opens audio context', async () => {
  Object.defineProperty(globalThis, 'navigator', { value: { mediaDevices: { getUserMedia: async () => { throw new DOMException('denied', 'NotAllowedError') } } }, configurable: true })
  const voice = new VoiceInput(() => {}, () => {}, () => assert.fail('audio should not be submitted'))
  await assert.rejects(voice.start(), { name: 'NotAllowedError' })
  assert.match(deviceError(new DOMException('denied', 'NotAllowedError'), '麦克风'), /权限未允许/)
  await voice.stop()
})
