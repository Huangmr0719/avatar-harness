import test from 'node:test'
import assert from 'node:assert/strict'
import {acceptComparison,formatMS,median,summarizeComparison,customScenario,newTurnDraft,reviewKey,checkScope} from '../src/promptComparison.ts'
const leg=(extra={})=>({variant:'legacy',status:'completed',text:'真实回答',revisions:1,repair_requests:0,requests:2,fallback:false,first_text_ms:1000,completion_ms:1200,candidate_checks:[{status:'fail'},{status:'manual'}],checks:[{status:'pass'},{status:'manual'}],...extra})
test('late snapshots cannot replace a different or newer comparison',()=>{
 const current={id:'live',revision:4,rounds:[]}
 assert.equal(acceptComparison(current,{id:'old',revision:90,rounds:[]},'live'),current)
 assert.equal(acceptComparison(current,{id:'live',revision:3,rounds:[]},'live'),current)
 const next={id:'live',revision:5,rounds:[]};assert.equal(acceptComparison(current,next,'live'),next)
})
test('only matched completed pairs contribute checks; semantic pending is not a pass',()=>{
 const run={rounds:[{conditions_match:true,legacy:leg()},{conditions_match:false,legacy:leg({first_text_ms:90000})},{conditions_match:true,legacy:leg({status:'running',checks:[{status:'pass'}]})}]}
 const s=summarizeComparison(run,'legacy');assert.equal(s.finalPass,1);assert.equal(s.finalTotal,1);assert.equal(s.manual,1);assert.equal(s.candidatePass,0);assert.equal(s.candidateTotal,1);assert.equal(s.firstMS,1000);assert.equal(s.completed,1)
})
test('failed and fallback replies remain visible instead of becoming perfect completion',()=>{
 const s=summarizeComparison({rounds:[{conditions_match:true,legacy:leg({fallback:true,checks:[{status:'fail'}]})},{conditions_match:true,legacy:leg({status:'error',text:''})}]},'legacy')
 assert.equal(s.errors,1);assert.equal(s.fallbacks,1);assert.equal(s.finalPass,0);assert.equal(s.finalTotal,1)
})
test('latency excludes missing observations and preserves actual zero',()=>{assert.equal(formatMS(), '—');assert.equal(formatMS(0),'0.00 秒');assert.equal(median([2000,1000,NaN]),1500);assert.equal(median([]),undefined)})
test('custom turns preserve fixed reference answers and explicit acceptance rules',()=>{
 const d={...newTurnDraft(),user:'  请概括  ',reference:'共同确认。',questions:0,sentences:2,required:'手机、500',forbidden:'电脑'}
 const s=customScenario('我的对照','cbt',[d]);assert.equal(s.turns[0].user,'请概括');assert.equal(s.turns[0].reference_answer,'共同确认。');assert.deepEqual(s.turns[0].checks.find(c=>c.kind==='contains_all').terms,['手机','500']);assert.equal(s.turns[0].checks.at(-1).kind,'manual');assert.notEqual(reviewKey(1,'legacy','candidate','x'),reviewKey(1,'current','final','x'))
})
test('format, lexical rules, action records, safety and semantic review remain separate',()=>{
 const checks=[{kind:'sentences',status:'pass'},{kind:'excludes',status:'fail'},{kind:'action_budget',status:'fail'},{kind:'risk_is',status:'pass'},{kind:'manual',status:'manual'}]
 const s=summarizeComparison({rounds:[{conditions_match:true,legacy:leg({checks})}]},'legacy')
 assert.deepEqual(s.groups.format,{pass:1,total:1});assert.deepEqual(s.groups.literal,{pass:0,total:1});assert.deepEqual(s.groups.record,{pass:0,total:1});assert.deepEqual(s.groups.safety,{pass:1,total:1});assert.equal(s.manual,1);assert.equal(s.groups.semantic.total,0)
 assert.equal(checkScope({kind:'manual'}),'semantic')
})
