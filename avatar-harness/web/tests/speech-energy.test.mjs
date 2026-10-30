import test from 'node:test'
import assert from 'node:assert/strict'
import {speechEnergyCurve} from '../src/speechEnergy.ts'

test('silence and low noise never trigger facial speech accents',()=>{
  for(const amplitude of [0,.001]){
    const samples=new Float32Array(16000).fill(amplitude)
    assert.ok([...speechEnergyCurve(samples,30,samples)].every(x=>x===0))
  }
})

test('energy accents follow the loud segment rather than shifting audio timestamps',()=>{
  const samples=new Float32Array(8000).fill(.02);samples.fill(.18,3200,4000)
  const curve=speechEnergyCurve(samples,10,samples)
  assert.equal(curve.length,10);assert.equal(curve[4],1)
  assert.ok(curve[0]<curve[4]&&curve[9]<curve[4])
  assert.ok([...curve].every(x=>Number.isFinite(x)&&x>=0&&x<=1))
  const scaled=Float32Array.from(samples,x=>x*2)
  const other=speechEnergyCurve(scaled,10,scaled)
  curve.forEach((x,i)=>assert.ok(Math.abs(x-other[i])<1e-6))
})
