import { computed, onMounted, onUnmounted, ref } from 'vue'
import { acceptComparison, customScenario, newTurnDraft, summarizeComparison, type ComparisonMetadata, type ComparisonRun, type ComparisonScenario, type Reviews } from './promptComparison'

async function api<T>(url: string, body?: unknown): Promise<T> {
  const response = await fetch(url, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const result = await response.json()
  if (!response.ok) throw new Error(result.detail || '请求未完成')
  return result
}
export function usePromptComparison() {
  const metadata = ref<ComparisonMetadata | null>(null), run = ref<ComparisonRun | null>(null), error = ref(''), connecting = ref(false), submitting = ref(false)
  const selected = ref(''), character = ref(''), repeats = ref(1), reviews = ref<Reviews>({})
  const title = ref('我的多轮对照'), mode = ref<ComparisonScenario['mode']>('humanistic'), drafts = ref([newTurnDraft(), newTurnDraft(), newTurnDraft()])
  let events: EventSource | undefined, generation = 0
  const running = computed(() => run.value?.status === 'running')
  const activeScenario = computed(() => selected.value === 'custom' ? customScenario(title.value, mode.value, drafts.value) : metadata.value?.scenarios.find(s => s.id === selected.value))
  const stats = computed(() => ({ legacy: summarizeComparison(run.value, 'legacy'), current: summarizeComparison(run.value, 'current') }))
  function loadReviews(id: string) {
    try { const value = JSON.parse(localStorage.getItem(`prompt-comparison-reviews:${id}`) || '{}'); reviews.value = Object.fromEntries(Object.entries(value).filter(([, v]) => v === 'pass' || v === 'fail')) as Reviews } catch { reviews.value = {} }
  }
  function review(key: string, value?: 'pass' | 'fail') {
    if (value) reviews.value = { ...reviews.value, [key]: value }; else { const copy = { ...reviews.value }; delete copy[key]; reviews.value = copy }
    if (run.value) try { localStorage.setItem(`prompt-comparison-reviews:${run.value.id}`, JSON.stringify(reviews.value)) } catch { /* Review remains available in the export. */ }
  }
  function attach(id: string) {
    events?.close(); const current = ++generation
    events = new EventSource(`/api/prompt-comparisons/${id}/events`)
    events.onopen = () => { if (current === generation) connecting.value = false }
    events.onmessage = event => {
      if (current !== generation) return
      try {
        run.value = acceptComparison(run.value, JSON.parse(event.data) as ComparisonRun, id)
        connecting.value = false
        if (run.value?.status !== 'running') events?.close()
      } catch { error.value = '对照记录格式异常，请重新加载记录。'; events?.close() }
    }
    events.onerror = () => { if (current === generation && running.value) connecting.value = true }
  }
  async function start() {
    if (!activeScenario.value || running.value || submitting.value) return
    submitting.value = true; error.value = ''; connecting.value = false
    try {
      const request = { ...(selected.value === 'custom' ? { scenario: activeScenario.value } : { scenario_id: selected.value }), character_id: character.value, repeats: repeats.value }
      const next = await api<ComparisonRun>('/api/prompt-comparisons', request)
      events?.close(); run.value = next; reviews.value = {}; history.replaceState(null, '', `/demo/prompts?run=${next.id}`); attach(next.id)
    } catch (e) { error.value = String(e instanceof Error ? e.message : e) } finally { submitting.value = false }
  }
  async function stop() {
    if (!run.value) return
    try { await api(`/api/prompt-comparisons/${run.value.id}/stop`, {}); error.value = '' } catch (e) { error.value = String(e) }
  }
  function download() {
    if (!run.value) return
    const payload = { ...run.value, manual_reviews: reviews.value, review_source: 'local_user_review_not_blinded', measurement: 'text latency; shared risk once per pair; frozen common history; not voice playback or intervention efficacy' }
    const url = URL.createObjectURL(new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })), a = document.createElement('a')
    a.href = url; a.download = `prompt-comparison-${run.value.id}.json`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 500)
  }
  const leave = () => { if (running.value && run.value) void fetch(`/api/prompt-comparisons/${run.value.id}/stop`, { method: 'POST', keepalive: true }) }
  onMounted(async () => {
    window.addEventListener('pagehide', leave)
    try {
      metadata.value = await api<ComparisonMetadata>('/api/prompt-comparisons/scenarios'); selected.value = metadata.value.scenarios[0]?.id || ''; character.value = metadata.value.conditions.character_id
      const id = new URLSearchParams(location.search).get('run')
      if (id) {
        run.value = await api<ComparisonRun>(`/api/prompt-comparisons/${encodeURIComponent(id)}`)
        character.value = run.value.conditions.character_id; repeats.value = run.value.repeats
        if (metadata.value.scenarios.some(s => s.id === run.value?.scenario.id)) selected.value = run.value.scenario.id
        loadReviews(id); if (running.value) attach(id)
      }
    } catch (e) { error.value = String(e instanceof Error ? e.message : e) }
  })
  onUnmounted(() => { generation++; events?.close(); leave(); window.removeEventListener('pagehide', leave) })
  return { metadata, run, error, connecting, submitting, selected, character, repeats, title, mode, drafts, running, activeScenario, stats, reviews, review, start, stop, download }
}
