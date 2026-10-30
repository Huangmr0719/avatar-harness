import test from 'node:test'
import assert from 'node:assert/strict'
import { SpeechBatcher } from '../src/lamSpeech.ts'

const packet = (pts, value = 10, rate = 24000) => ({ format: 'pcm_s16le', sample_rate: rate, channels: 1, pts_ms: pts, audio_base64: Buffer.alloc(rate / 10 * 2, value).toString('base64') })

test('speech batching preserves every real PCM sample, order and first PTS', () => {
  const output = [], batcher = new SpeechBatcher(p => output.push(p))
  const packets = [packet(0, 1), packet(100, 2), packet(200, 3), packet(300, 4)]
  packets.forEach(p => batcher.add(p))
  assert.equal(output.length, 1)
  assert.equal(output[0].pts_ms, 0)
  assert.deepEqual(Buffer.from(output[0].audio_base64, 'base64'), Buffer.concat(packets.map(p => Buffer.from(p.audio_base64, 'base64'))))
  batcher.reset()
})
test('a short tail flushes while cancellation drops pending old-turn samples', async () => {
  const output = [], batcher = new SpeechBatcher(p => output.push(p))
  batcher.add(packet(0)); batcher.reset()
  await new Promise(r => setTimeout(r, 100))
  assert.equal(output.length, 0)
  batcher.add(packet(700))
  await new Promise(r => setTimeout(r, 100))
  assert.equal(output.length, 1)
  assert.equal(output[0].pts_ms, 700)
})
test('discontinuous timestamps and sample-rate changes never merge', () => {
  const output = [], batcher = new SpeechBatcher(p => output.push(p))
  batcher.add(packet(0)); batcher.add(packet(400)); batcher.add(packet(500, 10, 16000)); batcher.flush()
  assert.deepEqual(output.map(p => p.pts_ms), [0, 400, 500])
})

test('large cloud bursts are split into one-window blocks without dropping samples or PTS', () => {
  const output=[],batcher=new SpeechBatcher(p=>output.push(p))
  const raw=Buffer.alloc(24000*2*1.2)
  for(let i=0;i<raw.length;i++)raw[i]=i%251
  batcher.add({format:'pcm_s16le',sample_rate:24000,channels:1,pts_ms:100,audio_base64:raw.toString('base64')});batcher.flush()
  assert.deepEqual(output.map(p=>p.pts_ms),[100,600,1100])
  assert.ok(output.every(p=>Buffer.from(p.audio_base64,'base64').length<=24000))
  assert.deepEqual(Buffer.concat(output.map(p=>Buffer.from(p.audio_base64,'base64'))),raw)
})

test('a partial block plus a cloud packet cannot exceed the inference window', () => {
  const output=[],batcher=new SpeechBatcher(p=>output.push(p))
  batcher.add({...packet(0),audio_base64:Buffer.alloc(24000*2*.3,1).toString('base64')})
  batcher.add({...packet(300),audio_base64:Buffer.alloc(24000*2*.4,2).toString('base64')});batcher.flush()
  assert.deepEqual(output.map(p=>p.pts_ms),[0,500])
  assert.deepEqual(output.map(p=>Buffer.from(p.audio_base64,'base64').length),[24000,9600])
})
