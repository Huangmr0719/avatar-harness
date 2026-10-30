import test from 'node:test'
import assert from 'node:assert/strict'
import { ARKIT_NAMES } from '../src/arkit.ts'
import { FACE_REGIONS, FaceRigControl } from '../src/faceRig.ts'

test('region controls cover every canonical channel once, including all five brow controls',()=>{
  const names=FACE_REGIONS.flatMap(region=>region.channels.map(([name])=>name))
  assert.equal(names.length,52);assert.equal(new Set(names).size,52)
  assert.deepEqual([...names].sort(),[...ARKIT_NAMES].sort())
  assert.equal(FACE_REGIONS[0].channels.length,5)
})

test('brow actions isolate forehead controls without taking over articulation',()=>{
  const rig=new FaceRigControl();rig.setSupported([...ARKIT_NAMES])
  rig.set('jawOpen',.4);rig.browAction('raise')
  assert.equal(rig.overrides.browOuterUpLeft,1);assert.equal(rig.overrides.browDownLeft,0)
  assert.equal(rig.overrides.jawOpen,.4)
  rig.browAction('frown')
  assert.equal(rig.overrides.browDownLeft,1);assert.equal(rig.overrides.browInnerUp,0)
  rig.reset('brows');assert.deepEqual(rig.overrides,{jawOpen:.4})
  rig.reset();assert.deepEqual(rig.overrides,{})
})

test('asset capability changes and invalid values cannot invent missing face channels',()=>{
  const rig=new FaceRigControl();rig.setSupported([...ARKIT_NAMES])
  rig.set('tongueOut',1);rig.setSupported(ARKIT_NAMES.filter(name=>name!=='tongueOut'))
  assert.deepEqual(rig.overrides,{})
  for(const [name,value]of [['tongueOut',.5],['foreheadWrinkle',1],['browInnerUp',NaN],['browInnerUp',2],['browInnerUp',-1]])assert.equal(rig.set(name,value),false)
  assert.deepEqual(rig.overrides,{})
  assert.equal(rig.set('browInnerUp',.7),true)
  const copy=rig.overrides;copy.browInnerUp=0
  assert.equal(rig.overrides.browInnerUp,.7)
})
