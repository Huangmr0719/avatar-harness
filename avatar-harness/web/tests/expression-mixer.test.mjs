import test from 'node:test'
import assert from 'node:assert/strict'
import { ARKIT_NAMES, arkitFrame } from '../src/arkit.ts'
import { applyBlink, exaggerateExpression, ExpressionMixer } from '../src/expressionMixer.ts'
import { BASIC_EMOTIONS } from '../src/expressionControl.ts'
import { ExpressionControl } from '../src/expressionControl.ts'

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
  assert.ok(accent.browInnerUp>.15&&accent.browOuterUpLeft>.2)
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

test('moderate emotions engage both brows and eyes before speech arrives',()=>{
  const channels={happy:['browOuterUpLeft','eyeSquintLeft'],sad:['browInnerUp','eyeSquintLeft'],angry:['browDownLeft','eyeSquintLeft'],fearful:['browInnerUp','eyeWideLeft'],surprised:['browOuterUpLeft','eyeWideLeft'],disgusted:['browDownLeft','eyeSquintLeft']}
  for(const [style,[brow,eye]] of Object.entries(channels)){
    const mixer=new ExpressionMixer();mixer.set(style,.6)
    const frame=mixer.sample(0)
    assert.ok(frame[brow]>.5,`${style} brow is too faint: ${frame[brow]}`)
    assert.ok(frame[eye]>.25,`${style} eyes are too faint: ${frame[eye]}`)
    assert.equal(frame.jawOpen,0)
  }
})

test('LLM emotion changes render upper-face motion without waiting for audio',()=>{
  const mixer=new ExpressionMixer()
  const control=new ExpressionControl(state=>mixer.set(state.style,state.intensity))
  control.begin('llm-turn')
  assert.equal(control.apply('llm-turn',{style:'surprised',intensity:.65}),true)
  const surprise=mixer.sample(0)
  assert.ok(surprise.browInnerUp>.7&&surprise.eyeWideLeft>.7)
  assert.equal(control.apply('old-turn',{style:'happy',intensity:1}),false)
  control.begin('next-turn')
  control.apply('next-turn',{style:'angry',intensity:.7})
  const angry=mixer.sample(1500,{jawOpen:.52,mouthClose:.16,mouthFunnel:.24})
  assert.ok(angry.browDownLeft>.8&&angry.eyeSquintLeft>.6)
  assert.ok(angry.eyeWideLeft<.001&&angry.browOuterUpLeft<.001)
  assert.equal(angry.jawOpen,.52);assert.equal(angry.mouthClose,.16);assert.equal(angry.mouthFunnel,.24)
  control.cancel()
  const neutral=mixer.sample(3000)
  assert.ok(neutral.browDownLeft<.001&&neutral.eyeSquintLeft<.001)
})

test('expressive intensity stays monotonic and zero releases affect even with PAD',()=>{
  let previous=0
  for(const intensity of [0,.1,.3,.6,.85,1]){
    const mixer=new ExpressionMixer();mixer.set('surprised',intensity)
    const face=mixer.sample(0)
    assert.ok(face.eyeWideLeft>=previous&&face.eyeWideLeft<=1)
    previous=face.eyeWideLeft
  }
  for(const style of [...BASIC_EMOTIONS,'natural']){
    const mixer=new ExpressionMixer();mixer.set(style,0,{pleasure:1,arousal:1})
    assert.ok(Object.values(mixer.sample(0)).every(value=>value===0),style)
  }
})

test('comparison mode isolates requested upper-face affect while retaining phonetic motion',()=>{
  const mixer=new ExpressionMixer();mixer.set('neutral',1)
  const audio={jawOpen:.5,mouthClose:.2,browInnerUp:.8,eyeSquintLeft:.9,mouthSmileLeft:.9}
  mixer.sample(0,audio,1)
  const neutral=mixer.sample(2000,audio,1,{speechAffect:false})
  assert.ok(neutral.browInnerUp<.001&&neutral.eyeSquintLeft<.001&&neutral.mouthSmileLeft<.001)
  assert.equal(neutral.jawOpen,.5);assert.equal(neutral.mouthClose,.2)
  mixer.set('surprised',1)
  const surprise=mixer.sample(4000,audio,1,{speechAffect:false})
  assert.ok(surprise.browInnerUp>.99&&surprise.eyeWideLeft>.99)
  assert.equal(surprise.jawOpen,.5)
})

test('theatrical rendering extends facial affect while retaining speech and neutral baseline',()=>{
  for(const style of BASIC_EMOTIONS){
    const mixer=new ExpressionMixer();mixer.set(style,1)
    const face=mixer.sample(0,{jawOpen:.65,mouthClose:.2,mouthFunnel:.3,mouthPucker:.15},0,{speechAffect:false})
    const before={...face}
    exaggerateExpression(face,style)
    for(const key of ['jawOpen','mouthClose','mouthFunnel','mouthPucker','eyeLookDownLeft','eyeBlinkLeft'])assert.equal(face[key],before[key])
    assert.ok(Object.values(face).every(value=>Number.isFinite(value)&&value>=0&&value<=1.65))
    if(style==='neutral')assert.deepEqual(face,before)
    if(style==='happy')assert.ok(face.mouthSmileLeft>1&&face.cheekSquintLeft>1)
    if(style==='angry')assert.ok(face.browDownLeft>1.5)
    if(style==='surprised')assert.ok(face.browOuterUpLeft>1.5&&face.eyeWideLeft>1)
    assert.ok(Object.values(before).every(value=>value<=1))
  }
})

test('theatrical eye opening never defeats a full blink',()=>{
  const mixer=new ExpressionMixer();mixer.set('surprised',1)
  const face=exaggerateExpression(applyBlink(mixer.sample(0),1),'surprised')
  assert.equal(face.eyeBlinkLeft,1);assert.equal(face.eyeBlinkRight,1)
  assert.equal(face.eyeWideLeft,0);assert.equal(face.eyeWideRight,0)
  assert.equal(face.eyeSquintLeft,0);assert.equal(face.eyeSquintRight,0)
  assert.ok(face.browInnerUp>1)
})
