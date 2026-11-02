export type VariantID = 'legacy' | 'current'
export type CheckStatus = 'pass' | 'fail' | 'manual' | 'not_run'
export interface ComparisonCheck { id: string; label: string; kind: string; terms?: string[]; limit?: number }
export type CheckScope = 'format' | 'literal' | 'record' | 'safety' | 'semantic'
export interface CheckResult extends ComparisonCheck { status: CheckStatus; detail: string; scope?: CheckScope }
export interface ComparisonMessage { role: 'user' | 'assistant'; content: string }
export interface ComparisonTurn { user: string; reference_answer: string; checks: ComparisonCheck[] }
export interface ComparisonScenario { id: string; title: string; mode: 'humanistic' | 'cbt' | 'mindfulness'; goal?: string; history?: ComparisonMessage[]; signals?: Record<string, unknown>; turns: ComparisonTurn[] }
export interface ComparisonLeg {
  variant: VariantID; status: string; text: string; candidate?: string; candidate_checks?: CheckResult[]; checks?: CheckResult[]
  first_text_ms?: number; completion_ms?: number; revisions: number; repair_requests: number; requests: number
  fallback: boolean; error?: string; conditions_hash?: string; phases?: { name: string; ok: boolean; elapsed_ms: number }[]
}
export interface ComparisonRound {
  index: number; repeat: number; turn: number; user: string; history: ComparisonMessage[]; reference_answer: string
  risk?: string; risk_error?: string; risk_ms: number; conditions_match: boolean; legacy: ComparisonLeg; current: ComparisonLeg
}
export interface ComparisonVariant { id: VariantID; label: string; behavior: string; voice: string; sha256: string }
export interface ComparisonConditions {
  model: string; parameters: Record<string, unknown>; provider_parameters_hash: string; character_id: string; persona_hash: string
  common_protocol_hash: string; history_limit: number; tokens: Record<string, number>; protocol: string
}
export interface ComparisonRun {
  id: string; revision: number; status: string; scenario: ComparisonScenario; repeats: number; conditions: ComparisonConditions
  variants: ComparisonVariant[]; rounds: ComparisonRound[]; error?: string; created_at: string
}
export interface ComparisonMetadata { scenarios: ComparisonScenario[]; variants: ComparisonVariant[]; conditions: ComparisonConditions; characters: { id: string; name: string }[] }
export type Reviews = Record<string, 'pass' | 'fail'>
export const variantNames: Record<VariantID, string> = { legacy: '旧版 System Prompt', current: '新版 System Prompt' }
export const variantIDs: VariantID[] = ['legacy', 'current']
export const checkNames: Record<CheckStatus, string> = { pass: '通过', fail: '未通过', manual: '待评阅', not_run: '未完成' }
export const scopeNames: Record<CheckScope, string> = { format: '格式', literal: '词面', record: '行动记录', safety: '安全', semantic: '语义评阅' }
export function checkScope(c: ComparisonCheck & { scope?: CheckScope }): CheckScope {
  return c.scope ?? (c.kind === 'manual' ? 'semantic' : ['risk_is', 'state_is'].includes(c.kind) ? 'safety' : c.kind === 'action_budget' ? 'record' : ['sentences', 'max_questions', 'max_characters', 'one_of'].includes(c.kind) ? 'format' : 'literal')
}
export function formatMS(ms?: number) { return ms == null || !Number.isFinite(ms) ? '—' : `${(ms / 1000).toFixed(2)} 秒` }
export function median(values: number[]): number | undefined {
  const sorted = values.filter(Number.isFinite).sort((a, b) => a - b)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length ? sorted.length % 2 ? sorted[middle] : (sorted[middle - 1]! + sorted[middle]!) / 2 : undefined
}
export function acceptComparison(previous: ComparisonRun | null, incoming: ComparisonRun, expectedID: string): ComparisonRun | null {
  if (incoming.id !== expectedID || !Number.isInteger(incoming.revision) || incoming.revision < 1 || !Array.isArray(incoming.rounds)) return previous
  if (previous?.id === incoming.id && previous.revision >= incoming.revision) return previous
  return incoming
}
export function reviewKey(round: number, variant: VariantID, stage: 'candidate' | 'final', check: string) { return `${round}:${variant}:${stage}:${check}` }
export function summarizeComparison(run: ComparisonRun | null, variant: VariantID) {
  const groups = Object.fromEntries(Object.keys(scopeNames).map(key => [key, { pass: 0, total: 0 }])) as Record<CheckScope, { pass: number; total: number }>
  const result = { completed: 0, errors: 0, fallbacks: 0, candidatePass: 0, candidateTotal: 0, finalPass: 0, finalTotal: 0, manual: 0, revisions: 0, repairs: 0, requests: 0, groups, firstMS: undefined as number | undefined, completionMS: undefined as number | undefined }
  const first: number[] = [], complete: number[] = []
  for (const round of run?.rounds ?? []) {
    const leg = round[variant]
    if (leg.status === 'error') result.errors++
    if (!round.conditions_match) continue
    result.revisions += leg.revisions; result.repairs += leg.repair_requests; result.requests += leg.requests
    if (leg.fallback) result.fallbacks++
    if (leg.status !== 'completed') continue
    result.completed++
    if (leg.first_text_ms != null) first.push(leg.first_text_ms)
    if (leg.completion_ms != null) complete.push(leg.completion_ms)
    for (const check of leg.candidate_checks ?? []) if (check.status === 'pass' || check.status === 'fail') { result.candidateTotal++; if (check.status === 'pass') result.candidatePass++ }
    for (const check of leg.checks ?? []) {
      if (check.status === 'manual') result.manual++
      if (check.status === 'pass' || check.status === 'fail') {
        const group = result.groups[checkScope(check)]; group.total++
        result.finalTotal++; if (check.status === 'pass') { result.finalPass++; group.pass++ }
      }
    }
  }
  result.firstMS = median(first); result.completionMS = median(complete)
  return result
}

export interface CustomTurnDraft { user: string; reference: string; questions: number; sentences: number; required: string; forbidden: string }
export function newTurnDraft(): CustomTurnDraft { return { user: '', reference: '', questions: 1, sentences: 0, required: '', forbidden: '' } }
export function customScenario(title: string, mode: ComparisonScenario['mode'], drafts: CustomTurnDraft[]): ComparisonScenario {
  const terms = (value: string) => value.split(/[、，,\n]/u).map(s => s.trim()).filter(Boolean)
  return { id: 'custom', title: title.trim(), mode, turns: drafts.map(d => {
    const checks: ComparisonCheck[] = [{ id: 'questions', label: `最多 ${d.questions} 个问号`, kind: 'max_questions', limit: d.questions }]
    if (d.sentences > 0) checks.push({ id: 'sentences', label: `恰好 ${d.sentences} 句`, kind: 'sentences', limit: d.sentences })
    if (terms(d.required).length) checks.push({ id: 'required', label: '包含指定内容', kind: 'contains_all', terms: terms(d.required) })
    if (terms(d.forbidden).length) checks.push({ id: 'forbidden', label: '不使用指定表述', kind: 'excludes', terms: terms(d.forbidden) })
    checks.push({ id: 'meaning', label: '准确完成当前要求，事实有依据且理解合适', kind: 'manual' })
    return { user: d.user.trim(), reference_answer: d.reference.trim(), checks }
  }) }
}
