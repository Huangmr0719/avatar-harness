import test from 'node:test'
import assert from 'node:assert/strict'
import { AttentionMotion } from '../src/attentionMotion.ts'

function sample(motion, from, to, listening, speaking) {
  const frames=[]
  for(let t=from;t<to;t+=16)frames.push(motion.sample(t,listening,speaking))
  return frames
}
test('listening gaze stays bounded and varies while a quiet microphone does not trigger nods',()=>{
  const motion=new AttentionMotion(()=>.8),frames=sample(motion,0,12000,true,false)
  assert.ok(frames.some(f=>Math.abs(f.gaze.yawDeg)>2))
  assert.ok(frames.some((f,i)=>i>150&&Math.abs(f.gaze.yawDeg)<.3))
  for(const frame of frames){
    assert.ok(Math.abs(frame.gaze.yawDeg)<=5&&Math.abs(frame.gaze.pitchDeg)<=3)
    assert.ok(Math.abs(frame.head[0])<.01)
    assert.ok(frame.head.every(Number.isFinite))
  }
})
test('a completed voiced phrase gets one soft acknowledgment, but a brief noise does not',()=>{
  const motion=new AttentionMotion(()=>.5)
  sample(motion,0,1000,true,true)
  const acknowledgment=sample(motion,1000,2500,true,false)
  assert.ok(Math.max(...acknowledgment.map(f=>f.head[0]))>.04)
  const later=sample(motion,2500,6000,true,false)
  assert.ok(later.every(f=>Math.abs(f.head[0])<.01))
  const noise=new AttentionMotion(()=>.5)
  sample(noise,0,100,true,true)
  assert.ok(sample(noise,100,2500,true,false).every(f=>Math.abs(f.head[0])<.01))
})
test('long speech receives spaced feedback and closing the microphone cancels pending nods',()=>{
  const motion=new AttentionMotion(()=>.5),frames=sample(motion,0,12000,true,true)
  let pulses=0,raised=false
  for(const frame of frames){const active=frame.head[0]>.02;if(active&&!raised)pulses++;raised=active}
  assert.ok(pulses>=2&&pulses<=3)
  sample(motion,12000,13000,false,false)
  const quiet=sample(motion,13000,17000,false,false)
  assert.ok(quiet.every(f=>Math.abs(f.head[0])<.001&&Math.abs(f.gaze.yawDeg)<.01))
  const cancelled=new AttentionMotion(()=>.5)
  sample(cancelled,0,1000,true,true);cancelled.sample(1000,true,false)
  assert.ok(sample(cancelled,1016,2400,false,false).every(f=>Math.abs(f.head[0])<.01))
})
