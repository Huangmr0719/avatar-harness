import test from 'node:test'
import assert from 'node:assert/strict'
import { LatencyTracker } from '../src/latency.ts'

test('latency uses actual playback total and first ordered segment, without summing stages', () => {
  let state; const tracker = new LatencyTracker(value => { state = value })
  tracker.begin('turn', 'voice')
  tracker.timing('turn', {stage:'tts.first_audio',segment:1,elapsed_ms:99})
  assert.equal(state.tts_ms, undefined)
  tracker.timing('turn', {stage:'llm.first_speech',elapsed_ms:688})
  tracker.timing('turn', {stage:'tts.first_audio',segment:0,elapsed_ms:558})
  tracker.record('turn', 'avatar_ms', 431)
  assert.equal(state.total_ms, undefined)
  tracker.record('turn', 'total_ms', 2010)
  tracker.record('turn', 'avatar_ms', 80)
  assert.deepEqual(state, {source:'voice',llm_ms:688,tts_ms:558,avatar_ms:431,total_ms:2010})
})
test('new turn resets metrics and cancelled or stale computations cannot overwrite them', () => {
  let state; const tracker = new LatencyTracker(value => { state = value })
  tracker.begin('old', 'text');tracker.record('old','avatar_ms',300);tracker.cancel()
  tracker.record('old','total_ms',1900)
  assert.equal(state.total_ms,undefined)
  tracker.begin('new','voice');tracker.record('old','avatar_ms',700)
  for(const bad of [-1,NaN,Infinity,'431'])tracker.record('new','avatar_ms',bad)
  assert.deepEqual(state,{source:'voice'})
  tracker.record('new','avatar_ms',0)
  assert.equal(state.avatar_ms,0)
  tracker.reset();assert.deepEqual(state,{})
})
