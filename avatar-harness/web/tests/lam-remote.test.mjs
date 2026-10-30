import test from 'node:test'
import assert from 'node:assert/strict'
import { LamSpeech } from '../src/lamSpeech.ts'
import { ARKIT_NAMES } from '../src/arkit.ts'

const health = {status:'ready', backend:'cuda', sample_rate:16000, input_samples:34133, names:[...ARKIT_NAMES]}
const audio = () => ({length:6400, duration:.4, sampleRate:16000, numberOfChannels:1, getChannelData:()=>new Float32Array(6400).fill(.1)})

test('remote LAM never creates a browser model worker and preserves frame timing and rolling context', async () => {
  const originalFetch=globalThis.fetch, originalWorker=globalThis.Worker
  const windows=[]
  globalThis.Worker=class {constructor(){throw Error('Remote mode loaded a local model')}}
  globalThis.fetch=async (_url, options) => {
    if(options.method==='GET') return Response.json(health)
    const bytes=Buffer.from(JSON.parse(options.body).audio_base64,'base64')
    const samples=new Float32Array(bytes.buffer,bytes.byteOffset,bytes.byteLength/4)
    windows.push(Float32Array.from(samples))
    return Response.json({weights:Array(64*52).fill(.2),elapsed_ms:18})
  }
  const driver=new LamSpeech('/api/sessions/test/driver')
  try {
    await driver.ready
    const first=await driver.prepare(audio(),100)
    assert.equal(driver.backend,'cuda')
    assert.equal(first.length,12)
    assert.equal(first[0].pts_ms,100)
    assert(first.at(-1).pts_ms<500)
    assert.equal(windows[0].length,34133)
    assert.equal(windows[0][0],0)
    assert(windows[0].at(-1)>.09)
    assert(first[0].blendshapes.jawOpen>.19)
    await driver.prepare(audio(),500)
    assert(windows[1][34133-12800]>.09,'prior context was lost between packets')
  } finally {driver.dispose();globalThis.fetch=originalFetch;globalThis.Worker=originalWorker}
})

test('reset aborts a pending remote window and prevents stale frames from reaching playback', async () => {
  const originalFetch=globalThis.fetch
  let started, begin=new Promise(resolve=>{started=resolve})
  globalThis.fetch=async (_url, options) => {
    if(options.method==='GET') return Response.json(health)
    started()
    return new Promise((_resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('Cancelled','AbortError')),{once:true}))
  }
  const driver=new LamSpeech('/api/sessions/test/driver')
  try {
    await driver.ready
    const pending=driver.prepare(audio(),0)
    await begin;driver.reset()
    assert.deepEqual(await pending,[])
  } finally {driver.dispose();globalThis.fetch=originalFetch}
})
