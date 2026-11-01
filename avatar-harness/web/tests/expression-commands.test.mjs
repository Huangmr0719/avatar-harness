import test from 'node:test'
import assert from 'node:assert/strict'
import { EXPRESSION_COMMANDS, expressionCommand, expressionSuggestions } from '../src/expressionCommands.ts'
import { BASIC_EMOTIONS, ExpressionControl } from '../src/expressionControl.ts'

test('slash lists all basic categories and an explicit way back to automatic expressions',()=>{
  const commands=expressionSuggestions('/')
  for(const id of [...BASIC_EMOTIONS,'auto'])assert.ok(commands.some(item=>item.id===id))
  assert.equal(expressionSuggestions('/表情').length,commands.length)
  assert.deepEqual(expressionSuggestions('今天怎么样'),[])
  assert.equal(expressionSuggestions('/愤怒')[0].id,'angry')
})
test('Chinese and English commands select the real expression control locally',()=>{
  const control=new ExpressionControl(()=>{})
  for(const item of EXPRESSION_COMMANDS){
    assert.equal(expressionCommand('/'+item.label),item.id)
    assert.equal(expressionCommand('/expression '+item.id),item.id)
    control.setMode(expressionCommand('/表情 '+item.label))
    assert.equal(control.state.mode,item.id)
  }
})
test('other slash commands and plain messages are not mistaken for expression previews',()=>{
  for(const text of ['/tool get_current_time','/help','hello','happy','/not-an-emotion','/'])assert.equal(expressionCommand(text),undefined)
  assert.deepEqual(expressionSuggestions('/tool'),[])
})
