import type { ExpressionMode } from './expressionControl.ts'

export const EXPRESSION_COMMANDS: { id: ExpressionMode; label: string; hint: string }[] = [
  { id: 'auto', label: '自动', hint: '恢复随对话变化' },
  { id: 'neutral', label: '中性', hint: '放松、平静' },
  { id: 'happy', label: '快乐', hint: '微笑与眉眼联动' },
  { id: 'sad', label: '悲伤', hint: '提内眉、嘴角下压' },
  { id: 'angry', label: '愤怒', hint: '压眉、收紧眼睑' },
  { id: 'fearful', label: '恐惧', hint: '抬眉、睁眼' },
  { id: 'surprised', label: '惊讶', hint: '提眉、睁大眼睛' },
  { id: 'disgusted', label: '厌恶', hint: '鼻翼与面颊联动' },
  { id: 'focused', label: '认真', hint: '专注的交流姿态' },
  { id: 'concerned', label: '关切', hint: '柔和的关心姿态' },
]
const term = (text: string) => text.trim().slice(1).replace(/^(?:表情|expression)(?:\s+|$)/i, '').trim().toLowerCase()
export function expressionSuggestions(text: string) {
  if (!text.trimStart().startsWith('/')) return []
  const query = term(text)
  return EXPRESSION_COMMANDS.filter(item => item.id.includes(query) || item.label.includes(query))
}
export function expressionCommand(text: string): ExpressionMode | undefined {
  if (!text.trimStart().startsWith('/')) return
  const query = term(text)
  return EXPRESSION_COMMANDS.find(item => item.id === query || item.label === query)?.id
}
