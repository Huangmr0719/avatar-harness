import test from 'node:test'
import assert from 'node:assert/strict'
import { appendErrorEntry, publicEntryText } from '../src/eventErrors.ts'

test('historical errors preserve distinct causes and merge repeated TTS segment failures', () => {
  const entries = []
  const event = (seq, turn_id, type, data) => ({ seq, turn_id, type, data, at: 1, session_id: 'session' })
  appendErrorEntry(entries, event(1, 'old', 'turn.error', { message: '数字人结构化回复不完整' }))
  const limited = { capability: 'tts', message: '百炼情感 TTS 错误 Throttling.RateQuota：Requests rate limit exceeded' }
  appendErrorEntry(entries, event(2, 'new', 'pipeline.error', limited))
  appendErrorEntry(entries, event(3, 'new', 'pipeline.error', limited))
  assert.equal(entries.length, 2)
  assert.match(publicEntryText(entries[0]), /回复格式异常/)
  assert.match(publicEntryText(entries[1]), /语音服务请求过于频繁/)
  assert.match(publicEntryText(entries[1]), /文字回复可继续阅读/)
  assert.equal(entries[1].text, limited.message, 'raw diagnostic must remain available')
  appendErrorEntry(entries, event(4, 'next', 'pipeline.error', limited))
  assert.equal(entries.length, 3, 'different turns must retain their own errors')
})

test('public messages retain text and distinguish generic capability failures', () => {
  assert.equal(publicEntryText({ text: '正常回复' }), '正常回复')
  assert.match(publicEntryText({ text: 'private endpoint failure', status: 'error', capability: 'tts' }), /语音生成失败/)
  assert.match(publicEntryText({ text: 'private endpoint failure', status: 'error', capability: 'avatar' }), /人物动作生成失败/)
  assert.doesNotMatch(publicEntryText({ text: 'private endpoint failure', status: 'error', capability: 'llm' }), /private/)
})
