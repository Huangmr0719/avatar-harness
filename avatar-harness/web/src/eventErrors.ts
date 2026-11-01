import type { Entry, HarnessEvent } from './types.ts'

function errorCode(message: string, capability?: string): string {
  if (capability === 'tts' && /Throttling\.(RateQuota|BurstRate)|Requests rate limit exceeded|请求.*限流/i.test(message)) return 'rate_limited'
  if (/结构化回复|speech.*(?:转义|字符串|控制字符)/.test(message)) return 'invalid_reply'
  return 'failed'
}

// Preserve the raw reason for development diagnostics. Public history displays
// the failed capability, including for events saved before structured codes.
export function appendErrorEntry(entries: Entry[], event: HarnessEvent): void {
  const text = String(event.data.message || '操作失败')
  const capability = event.data.capability || (event.type === 'turn.error' ? 'llm' : undefined)
  const code = event.data.code || errorCode(text, capability)
  const turn = event.turn_id || undefined
  if (turn && entries.some(entry => entry.kind === 'notice' && entry.status === 'error' && entry.turn_id === turn && entry.capability === capability && entry.code === code)) return
  entries.push({ id: `error-${event.seq}`, at: event.at, kind: 'notice', text, status: 'error', turn_id: turn, capability, code })
}

export function publicEntryText(entry: Pick<Entry, 'text' | 'status' | 'source' | 'capability' | 'code'>): string {
  if (entry.status === 'error') {
    if (entry.code === 'invalid_reply') return '回复格式异常，本轮回答未完整完成，请重新发送。'
    if (entry.capability === 'tts') return entry.code === 'rate_limited'
      ? '语音服务请求过于频繁，本次语音未能完整播放；文字回复可继续阅读。'
      : '本次语音生成失败，文字回复可继续阅读。'
    if (entry.capability === 'avatar') return '人物动作生成失败，请稍后重试。'
    if (entry.capability === 'llm') return '本次回复未能完成，请重试。'
    return '暂时无法完成，请稍后再试。'
  }
  if (entry.status === 'timeout') return '回应较慢，请稍候。'
  if (entry.source === 'local_command' && entry.text.trim().startsWith('{')) return '操作已完成。'
  return entry.text
}
