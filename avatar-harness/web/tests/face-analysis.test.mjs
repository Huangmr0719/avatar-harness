import test from 'node:test'
import assert from 'node:assert/strict'
import { FaceAnalysis } from '../src/faceAnalysis.ts'

const tick = () => new Promise(resolve => setImmediate(resolve))
test('stopping analysis releases the server token and rejects a late frame result',async t=>{
 const originalFetch=globalThis.fetch,originalDocument=globalThis.document
 t.after(()=>{globalThis.fetch=originalFetch;globalThis.document=originalDocument})
 globalThis.document={visibilityState:'visible'}
 const requests=[],reports=[];let finish
 globalThis.fetch=async(url,opts)=>{
  const data=JSON.parse(opts.body);requests.push(data)
  if(data.operation==='start')return{ok:true,json:async()=>({token:'stream-one'})}
  if(data.operation==='frame')return await new Promise(resolve=>{finish=()=>resolve({ok:true,json:async()=>({face_detected:true,au_intensities:{AU12:4}})})})
  return{ok:true,json:async()=>({status:'off'})}
 }
 const analysis=new FaceAnalysis((...event)=>reports.push(event))
 await analysis.start('session-one',async()=>'public-camera-fixture');await tick()
 assert.equal(requests.filter(r=>r.operation==='frame').length,1)
 analysis.stop();finish();await tick()
 assert.equal(requests.at(-1).operation,'stop')
 assert.equal(requests.at(-1).token,'stream-one')
 assert.ok(!reports.some(([state])=>state==='on'),'late inference revived the old camera')
})
test('analysis errors clear displayed observations and stop further upload',async t=>{
 const originalFetch=globalThis.fetch,originalDocument=globalThis.document
 t.after(()=>{globalThis.fetch=originalFetch;globalThis.document=originalDocument})
 globalThis.document={visibilityState:'visible'}
 const reports=[],requests=[]
 globalThis.fetch=async(url,opts)=>{
  const data=JSON.parse(opts.body);requests.push(data)
  return data.operation==='frame'?{ok:false,json:async()=>({detail:'connection lost'})}:{ok:true,json:async()=>({token:'stream-two'})}
 }
 const analysis=new FaceAnalysis((...event)=>reports.push(event))
 await analysis.start('s',async()=>'fixture');await tick()
 assert.equal(reports.at(-1)[0],'error');assert.equal(reports.at(-1)[1],undefined)
 assert.equal(requests.at(-1).operation,'stop');analysis.stop()
})
