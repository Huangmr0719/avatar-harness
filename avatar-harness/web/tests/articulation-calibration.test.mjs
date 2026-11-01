import test from 'node:test'
import assert from 'node:assert/strict'
import { calibrateSpeechJaw } from '../src/articulationCalibration.ts'

test('asset calibration preserves small speech openings and smoothly bounds large ones',()=>{
 const values={jawOpen:.1,mouthClose:.4,mouthFunnel:.8};calibrateSpeechJaw(values,.32)
 assert.equal(values.jawOpen,.1)
 let previous=0
 for(let jaw=0;jaw<=1;jaw+=.01){const frame={jawOpen:jaw};calibrateSpeechJaw(frame,.32);assert.ok(frame.jawOpen>=previous);assert.ok(frame.jawOpen<=.32);previous=frame.jawOpen}
 assert.equal(values.mouthClose,.4);assert.equal(values.mouthFunnel,.8)
 const otherAsset={jawOpen:.9};calibrateSpeechJaw(otherAsset);assert.equal(otherAsset.jawOpen,.9)
})
