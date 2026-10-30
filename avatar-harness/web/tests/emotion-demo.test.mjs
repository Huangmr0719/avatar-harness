import test from 'node:test'
import assert from 'node:assert/strict'
import { EmotionDemo, EMOTION_DEMO, DEMO_TEXT } from '../src/emotionDemo.ts'

const deferred=()=>{let resolve;const promise=new Promise(done=>{resolve=done});return {promise,resolve}}

test('seven maximum-intensity categories speak the same comparison sentence in playback order',async()=>{
  assert.equal(EMOTION_DEMO.length,7)
  assert.equal(new Set(EMOTION_DEMO.map(x=>x.style)).size,7)
  assert.ok(EMOTION_DEMO.every(x=>x.intensity===1&&x.text===DEMO_TEXT))
  const first=deferred(),started=deferred(),calls=[],states=[]
  let cleaned=0
  const demo=new EmotionDemo({
    changed:s=>states.push(s),reset:()=>{},finish:()=>cleaned++,hold:async()=>{},
    synthesize:async step=>{calls.push('tts:'+step.style);return {audio_base64:'actual-fixture'}},
    play:async(_,step)=>{calls.push('play:'+step.style);if(step.style==='neutral'){started.resolve();await first.promise}},
  })
  const run=demo.start();await started.promise
  assert.deepEqual(calls,['tts:neutral','play:neutral'])
  assert.equal(states.at(-1).phase,'playing')
  first.resolve();await run
  assert.deepEqual(calls,EMOTION_DEMO.flatMap(x=>['tts:'+x.style,'play:'+x.style]))
  assert.equal(states.at(-1).phase,'complete');assert.equal(cleaned,1)
})

test('stop during synthesis discards late audio and never starts the next emotion',async()=>{
  const late=deferred(),states=[];let played=0,cleaned=0,signal
  const demo=new EmotionDemo({changed:s=>states.push(s),reset:()=>{},finish:()=>cleaned++,
    synthesize:async(_,s)=>{signal=s;return late.promise},play:async()=>{played++},hold:async()=>{}})
  const run=demo.start();demo.cancel()
  assert.equal(signal.aborted,true);assert.equal(cleaned,1);assert.equal(states.at(-1).phase,'cancelled')
  late.resolve({audio_base64:'late'});await run
  assert.equal(played,0);assert.equal(cleaned,1);assert.equal(states.at(-1).phase,'cancelled')
})

test('a failed voice stops the sequence and restores the normal controls',async()=>{
  const states=[];let requests=0,restored=0
  const demo=new EmotionDemo({changed:s=>states.push(s),reset:()=>{},finish:()=>restored++,
    synthesize:async()=>{requests++;throw new Error('provider unavailable')},play:async()=>{},hold:async()=>{}})
  await demo.start()
  assert.equal(requests,1);assert.equal(restored,1);assert.equal(states.at(-1).phase,'error')
  assert.match(states.at(-1).message,/provider unavailable/)
})
