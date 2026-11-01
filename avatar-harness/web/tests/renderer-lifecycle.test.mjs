import test from 'node:test'
import assert from 'node:assert/strict'
import { disposeLamRenderer } from '../src/rendererLifecycle.ts'

test('viewer releases its canvas before wrapper cleanup and queued frames are cancelled first',async()=>{
  let finish,attached=true,disposed=false,wrapperDisposed=false
  const sorting=new Promise(resolve=>{finish=resolve}),cancelled=[]
  const viewer={requestFrameId:42,async dispose(){
    if(disposed)return
    await sorting
    assert.equal(attached,true,'canvas was already removed by the wrapper')
    attached=false;disposed=true
  }}
  const wrapper={viewer,dispose(){
    assert.equal(disposed,true,'wrapper disposed before its asynchronous viewer')
    if(attached)attached=false
    wrapperDisposed=true
  }}
  const cleanup=disposeLamRenderer(wrapper,id=>cancelled.push(id))
  assert.deepEqual(cancelled,[42]);assert.equal(wrapperDisposed,false)
  finish();await cleanup
  assert.equal(attached,false);assert.equal(wrapperDisposed,true)
})
