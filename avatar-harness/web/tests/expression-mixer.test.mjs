import test from 'node:test'
import assert from 'node:assert/strict'
import { ARKIT_NAMES, arkitFrame } from '../src/arkit.ts'
import { applyBlink, ExpressionMixer } from '../src/expressionMixer.ts'
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

test('speech accents move a neutral brow, decay in silence, and never replace articulation',()=>{
  const mixer=new ExpressionMixer();mixer.set('neutral',.6)
  mixer.sample(0,{jawOpen:.1},0)
  const accent=mixer.sample(100,{jawOpen:.65},1)
  assert.ok(accent.browInnerUp>.02&&accent.browOuterUpLeft>.02)
  assert.equal(accent.jawOpen,.65)
  const quiet=mixer.sample(2500,{},0)
  assert.ok(quiet.browInnerUp<.001&&quiet.browOuterUpLeft<.001)
  assert.equal(quiet.jawOpen,0)
})

test('requested affect controls cheeks and lids even with competing audio predictions',()=>{
  const mixer=new ExpressionMixer();mixer.set('happy',1)
  const frame=mixer.sample(0,{mouthFrownLeft:1,eyeWideLeft:1,mouthClose:.4,jawLeft:.2})
  assert.ok(frame.cheekSquintLeft>.6&&frame.eyeSquintLeft>.3)
  assert.ok(frame.mouthFrownLeft<.2&&frame.eyeWideLeft<.2)
  assert.equal(frame.mouthClose,.4);assert.equal(frame.jawLeft,.2)
})

test('a full blink closes raised lids while preserving the rest of the expression',()=>{
  const mixer=new ExpressionMixer();mixer.set('surprised',1)
  const face=mixer.sample(0),blink=applyBlink({...face},1)
  assert.ok(face.eyeWideLeft>.9)
  assert.equal(blink.eyeWideLeft,0);assert.equal(blink.eyeWideRight,0)
  assert.equal(blink.eyeBlinkLeft,1);assert.equal(blink.eyeBlinkRight,1)
  assert.equal(blink.browInnerUp,face.browInnerUp)
})
