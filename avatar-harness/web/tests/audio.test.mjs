import assert from 'node:assert/strict'
import test from 'node:test'
import { AudioPlayer, firstVoicedSample } from '../src/audio.ts'

const decodedBuffer = id => ({ id, duration: .01, sampleRate: 24000, numberOfChannels: 1, length: 240, getChannelData: () => new Float32Array(240).fill(.1) })

function deferred() {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

function audioFixture() {
  const firstDecode = deferred()
  const decodingStarted = deferred()
  const decoded = []
  const started = []
  const sources = []
  const contexts = []
  globalThis.AudioContext = class {
    constructor() { contexts.push(this) }
    state = 'running'
    currentTime = 0
    destination = {}
    decodeAudioData(bytes) {
      const id = new Uint8Array(bytes)[0]
      decoded.push(id)
      if (id === 1) { decodingStarted.resolve(); return firstDecode.promise }
      return Promise.resolve(decodedBuffer(id))
    }
    createBufferSource() {
      const source = {
        connect() {},
        start() { started.push(source.buffer.id) },
        stop() {},
        onended: null,
      }
      sources.push(source)
      return source
    }
  }
  return { firstDecode, decodingStarted, decoded, started, sources, contexts }
}

const packet = id => ({ format: 'wav', audio_base64: Buffer.from([id]).toString('base64') })

test('audio keeps server order through asynchronous WAV decoding and remains stoppable after scheduling', async () => {
  const fixture = audioFixture()
  const states = []
  const player = new AudioPlayer(value => states.push(value))
  const first = player.play(packet(1))
  const second = player.play(packet(2))
  await fixture.decodingStarted.promise
  assert.deepEqual(fixture.decoded, [1])
  fixture.firstDecode.resolve(decodedBuffer(1))
  await Promise.all([first, second])
  assert.deepEqual(fixture.started, [1, 2])
  assert.equal(states.at(-1), true)
  fixture.sources[0].onended()
  assert.equal(states.at(-1), true)
  fixture.sources[1].onended()
  assert.equal(states.at(-1), false)
})

test('demo completion waits for actual source end and its wait can be interrupted',async()=>{
  const fixture=audioFixture(),player=new AudioPlayer(),controller=new AbortController()
  await player.play(packet(2))
  let complete=false
  const done=player.waitUntilIdle(controller.signal).then(()=>{complete=true})
  await Promise.resolve();assert.equal(complete,false)
  fixture.sources[0].onended();await done;assert.equal(complete,true)
  await player.play(packet(3))
  const stop=new AbortController(),cancelled=player.waitUntilIdle(stop.signal)
  stop.abort();await assert.rejects(cancelled,{name:'AbortError'});player.stop()
})

test('stop rejects audio still decoding without blocking a new turn', async () => {
  const fixture = audioFixture()
  const states = []
  const player = new AudioPlayer(value => states.push(value))
  const oldTurn = player.play(packet(1))
  await fixture.decodingStarted.promise
  player.stop()
  assert.equal(states.at(-1), false)
  await player.play(packet(2))
  fixture.firstDecode.resolve(decodedBuffer(1))
  await oldTurn
  assert.deepEqual(fixture.started, [2])
  assert.equal(states.at(-1), true)
  player.stop()
  assert.equal(states.at(-1), false)
})

test('response timing observes the running audio clock instead of enqueue time', async () => {
  const fixture = audioFixture(), observed = []
  const player = new AudioPlayer(() => {}, () => {}, segment => observed.push(segment))
  await player.play(packet(2))
  assert.equal(observed.length, 0)
  fixture.contexts[0].currentTime = .05
  await new Promise(resolve => setTimeout(resolve, 70))
  assert.equal(observed.length, 1)
  player.stop()
})

test('stopping before scheduled playback prevents a false response report', async () => {
  const fixture = audioFixture(), observed = []
  const player = new AudioPlayer(() => {}, () => {}, segment => observed.push(segment))
  await player.play(packet(2))
  player.stop()
  fixture.contexts[0].currentTime = .5
  await new Promise(resolve => setTimeout(resolve, 70))
  assert.equal(observed.length, 0)
})

test('initial silent samples cannot count as a meaningful voice response', () => {
  const samples = new Float32Array(24000)
  const buffer = { sampleRate: 24000, numberOfChannels: 1, length: samples.length, getChannelData: () => samples }
  assert.equal(firstVoicedSample(buffer), null)
  samples.fill(.1, 2400)
  assert.equal(firstVoicedSample(buffer), .1)
})

test('motion preparation completes before playback and cancelled inference cannot start old audio', async () => {
  const fixture = audioFixture(), prepared = deferred(), entered = deferred()
  const player = new AudioPlayer(() => {}, () => {}, () => {}, async buffer => {
    if (buffer.id === 2) { entered.resolve(); await prepared.promise }
  })
  const old = player.play(packet(2))
  await entered.promise
  assert.deepEqual(fixture.started, [])
  player.stop()
  prepared.resolve()
  await old
  assert.deepEqual(fixture.started, [])
  await player.play(packet(3))
  assert.deepEqual(fixture.started, [3])
  player.stop()
})
