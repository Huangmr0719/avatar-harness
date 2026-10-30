import test from 'node:test'
import assert from 'node:assert/strict'
import { ARKIT_NAMES, arkitFrame } from '../src/arkit.ts'
import { ExpressionMixer } from '../src/expressionMixer.ts'
import { BASIC_EMOTIONS } from '../src/expressionControl.ts'

test('official LAM channel order maps jaw and eyes without position ambiguity', () => {
  assert.equal(ARKIT_NAMES.length,52)
  assert.equal(new Set(ARKIT_NAMES).size,52)
  assert.equal(ARKIT_NAMES[24],'jawOpen')
  assert.equal(ARKIT_NAMES[8],'eyeBlinkLeft')
  const weights=new Float32Array(52);weights[24]=.6;weights[8]=1
  const frame=arkitFrame(weights)
  assert.ok(Math.abs(frame.jawOpen-.6)<1e-6)
  assert.equal(frame.eyeBlinkLeft,1)
  assert.equal(frame.eyeBlinkRight,0)
})
test('full face emotions preserve phonetic articulation and explicitly close silent jaw', () => {
  const mixer=new ExpressionMixer();mixer.set('happy',.8)
  const frame=mixer.sample(0,{jawOpen:.72,mouthClose:.18,mouthFunnel:.3})
  assert.equal(Object.keys(frame).length,52)
  assert.equal(frame.jawOpen,.72);assert.equal(frame.mouthClose,.18);assert.equal(frame.mouthFunnel,.3)
  assert.ok(frame.cheekSquintLeft>0&&frame.eyeSquintLeft>0&&frame.mouthSmileLeft>0)
  assert.equal(mixer.sample(16).jawOpen,0)
})
test('expression changes interpolate over time and then release previous facial channels', () => {
  const mixer=new ExpressionMixer();mixer.set('happy',1);const happy=mixer.sample(100)
  mixer.set('concerned',1)
  const first=mixer.sample(116),settled=mixer.sample(1200)
  assert.ok(first.mouthSmileLeft>settled.mouthSmileLeft)
  assert.ok(first.browInnerUp>0&&first.browInnerUp<settled.browInnerUp)
  assert.ok(settled.mouthSmileLeft<.001&&settled.browInnerUp>.3)
  mixer.set('natural',0);const neutral=mixer.sample(3000)
  assert.ok(neutral.mouthFrownLeft<.001&&neutral.browInnerUp<.001)
  assert.ok(happy.mouthSmileLeft>.4)
})

test('seven emotions have distinct bounded face poses without overriding phonetic channels', () => {
  const signatures=new Set()
  for(const emotion of BASIC_EMOTIONS){
    const mixer=new ExpressionMixer();mixer.set(emotion,1)
    const face=mixer.sample(0,{jawOpen:.42,mouthClose:.16,mouthFunnel:.21,mouthPucker:.12,tongueOut:.08})
    signatures.add(JSON.stringify(face))
    for(const value of Object.values(face))assert.ok(Number.isFinite(value)&&value>=0&&value<=1)
    for(const [key,value] of Object.entries({jawOpen:.42,mouthClose:.16,mouthFunnel:.21,mouthPucker:.12,tongueOut:.08}))assert.equal(face[key],value,emotion+': '+key)
    const zero=new ExpressionMixer();zero.set(emotion,0)
    assert.ok(Object.values(zero.sample(0)).every(value=>value===0))
  }
  assert.equal(signatures.size,7)
})
