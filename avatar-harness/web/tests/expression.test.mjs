import test from 'node:test'
import assert from 'node:assert/strict'
import { BASIC_EMOTIONS, ExpressionControl } from '../src/expressionControl.ts'

test('all seven basic emotions pass from automatic control to manual preview',()=>{
  const control=new ExpressionControl(()=>{});control.begin('turn')
  assert.equal(BASIC_EMOTIONS.length,7)
  for(const style of BASIC_EMOTIONS){
    control.setMode('auto')
    assert.equal(control.apply('turn',{style,intensity:.65}),true)
    assert.equal(control.state.style,style);assert.equal(control.state.intensity,.65)
    control.setMode(style);assert.equal(control.state.style,style)
    assert.equal(control.state.source,'manual')
  }
  control.cancel()
})

test('automatic expressions reject old turns and cancellation restores neutral', () => {
  const changed=[], control=new ExpressionControl(state=>changed.push(state))
  control.begin('new')
  assert.equal(control.apply('old',{style:'happy',intensity:.8}),false)
  assert.equal(control.apply('new',{style:'concerned',intensity:.6}),true)
  assert.equal(changed.at(-1).style,'concerned')
  control.cancel()
  assert.equal(control.apply('new',{style:'happy'}),false)
  assert.equal(changed.at(-1).style,'neutral')
})
test('manual choice overrides automatic emotion until user returns to auto', () => {
  const control=new ExpressionControl(()=>{})
  control.begin('turn');control.setMode('focused')
  control.apply('turn',{style:'happy',intensity:.8})
  assert.equal(control.state.style,'focused')
  assert.equal(control.state.source,'manual')
  control.setMode('auto')
  assert.equal(control.state.style,'happy')
  control.cancel()
})
test('invalid controls never alter the face and elapsed automatic emotion resets', async () => {
  const control=new ExpressionControl(()=>{})
  control.begin('turn')
  for(const bad of [{style:'evil'},{style:'happy',intensity:NaN},{style:'happy',intensity:2},{style:'happy',duration_ms:Infinity}]) assert.equal(control.apply('turn',bad),false)
  assert.equal(control.state.style,'neutral')
  control.apply('turn',{style:'happy',intensity:.5,duration_ms:500})
  await new Promise(r=>setTimeout(r,550))
  assert.equal(control.state.style,'neutral')
  control.cancel()
})
