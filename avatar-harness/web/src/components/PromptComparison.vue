<script setup lang="ts">
import { computed } from 'vue'
import ComparisonCheckCell from './ComparisonCheckCell.vue'
import { usePromptComparison } from '../usePromptComparison'
import { formatMS, newTurnDraft, reviewKey, variantIDs, variantNames, type CheckResult, type ComparisonLeg, type ComparisonRound, type VariantID } from '../promptComparison'
import '../promptComparison.css'
const { metadata, run, error, connecting, submitting, selected, character, repeats, title, mode, drafts, running, activeScenario, stats, reviews, review, start, stop, download } = usePromptComparison()
const conditions = computed(() => run.value?.conditions ?? metadata.value?.conditions)
const variants = computed(() => run.value?.variants ?? metadata.value?.variants ?? [])
const riskNames: Record<string, string> = { ordinary: '普通交流', research: '研究讨论', uncertain: '需要确认', urgent: '紧急支持' }
const statusNames: Record<string, string> = { running: '正在生成', completed: '已完成', cancelled: '已停止', error: '未完成' }
const completed = computed(() => run.value?.rounds.reduce((n, r) => n + Number(r.legacy.status !== 'running') + Number(r.current.status !== 'running'), 0) ?? 0)
const total = computed(() => run.value ? run.value.scenario.turns.length * run.value.repeats * 2 : 0)
const manualDone = computed(() => Object.keys(reviews.value).length)
function checks(round: ComparisonRound, variant: VariantID): CheckResult[] {
  return round[variant].checks ?? run.value?.scenario.turns[round.turn - 1]?.checks.map(c => ({ ...c, status: 'not_run' as const, detail: '尚未完成' })) ?? []
}
function candidate(leg: ComparisonLeg, id: string) { return leg.candidate_checks?.find(c => c.id === id) }
const fraction = (n: number, total: number) => total ? `${n} / ${total}` : '—'
</script>

<template>
  <div class="prompt-compare">
    <header class="pc-topbar"><a href="/" class="pc-back">← 数字人工作台</a><span>独立对照演示</span><a href="/demo/emotions">情绪演示 ↗</a></header>
    <main class="pc-main">
      <section class="pc-intro">
        <div><p class="pc-eyebrow">同题 · 同上下文 · 并行生成</p><h1>System Prompt 对照</h1><p class="pc-description">只切换行为与语音表达策略。比较两版怎样理解要求、承接上文、使用约束，并查看审校究竟改了多少。</p></div>
        <div class="pc-model"><small>共同底座模型</small><strong :title="conditions?.model">{{ conditions?.model || '正在读取配置…' }}</strong><span>文字对照 · 不合成语音</span></div>
      </section>

      <section class="pc-config" aria-label="对照设置">
        <div class="pc-toolbar">
          <label class="pc-scenario-label">多轮情境<select v-model="selected" aria-label="多轮情境" :disabled="running || submitting"><option v-for="s in metadata?.scenarios" :key="s.id" :value="s.id">{{ s.title }}</option><option value="custom">自定义多轮对话</option></select></label>
          <label>共同角色<select v-model="character" aria-label="共同角色" :disabled="running || submitting"><option v-for="c in metadata?.characters" :key="c.id" :value="c.id">{{ c.name }}</option></select></label>
          <label class="pc-repeat-label">重复次数<select v-model.number="repeats" aria-label="重复次数" :disabled="running || submitting"><option :value="1">1 次</option><option :value="2">2 次</option><option :value="3">3 次</option></select></label>
          <button class="pc-primary" :disabled="!metadata || running || submitting" @click="start">{{ submitting ? '准备中…' : '运行本组对照' }}</button>
          <button v-if="running" class="pc-stop" @click="stop">停止对照</button>
          <button v-if="run" class="pc-secondary" @click="download">导出结果</button>
        </div>
        <div class="pc-controls-summary"><span>{{ activeScenario?.turns.length || 0 }} 轮 × 两版</span><span>相同历史与记忆</span><span>共用风险检查</span><span>相同审校策略</span><span>{{ activeScenario?.signals ? '共用固定感知观察' : '不注入感知观察' }}</span></div>
        <p class="pc-protocol">每轮使用预先约定的共同历史；两版刚生成的回答不会进入下一轮，避免上下文逐渐不同。窗口外原话检索同样保留，对照结果不写入正常会话。</p>

        <div v-if="selected === 'custom'" class="pc-custom" aria-label="自定义对话">
          <div class="pc-custom-heading"><label>情境名称<input v-model="title" maxlength="100" :disabled="running"></label><label>共同交流方式<select v-model="mode" :disabled="running"><option value="humanistic">倾听支持</option><option value="cbt">认知梳理</option><option value="mindfulness">注意练习</option></select></label></div>
          <fieldset v-for="(draft, i) in drafts" :key="i" :disabled="running"><legend>第 {{ i + 1 }} 轮</legend>
            <label>用户话语<textarea v-model="draft.user" rows="2" maxlength="2000" placeholder="输入本轮两侧都会收到的话语" /></label>
            <label>下一轮共同参考答复<textarea v-model="draft.reference" rows="2" maxlength="2000" placeholder="供下一轮承接；不采用任何一侧生成的回答。留空时使用“收到。”" /></label>
            <details><summary>设置本轮验收</summary><div class="pc-custom-checks"><label>最多几个问号<select v-model.number="draft.questions"><option :value="0">0</option><option :value="1">1</option></select></label><label>要求句数（0 为不限）<input v-model.number="draft.sentences" type="number" min="0" max="12"></label><label>必须包含<input v-model="draft.required" placeholder="多个内容用顿号分隔"></label><label>不得包含<input v-model="draft.forbidden" placeholder="多个表述用顿号分隔"></label></div></details>
            <button v-if="drafts.length > 1" class="pc-text-button" @click="drafts.splice(i, 1)">移除此轮</button>
          </fieldset>
          <button class="pc-secondary" :disabled="running || drafts.length >= 6" @click="drafts.push(newTurnDraft())">＋ 添加一轮</button>
        </div>

        <details v-else class="pc-preview"><summary>查看 {{ activeScenario?.turns.length || 0 }} 轮输入与验收约定</summary><ol><li v-for="(turn, i) in activeScenario?.turns" :key="i"><p>{{ turn.user }}</p><small>共同参考答复：{{ turn.reference_answer || '收到。' }}</small><ul><li v-for="check in turn.checks" :key="check.id">{{ check.label }}{{ check.kind === 'manual' ? ' · 人工项' : '' }}</li></ul></li></ol></details>
        <details class="pc-specification"><summary>核对两版 Prompt 与共同条件</summary><p>旧版行为与语音文本取自历史版本 42a366e；当前的输出格式、记忆解释、感知解释、风险与审校规则两侧共用。工具关闭，模型请求参数一致。</p><div class="pc-prompt-grid"><section v-for="variant in variants" :key="variant.id"><h3>{{ variant.label }}</h3><pre>{{ variant.behavior }}{{ variant.voice }}</pre><small>版本指纹：{{ variant.sha256 }}</small></section></div><dl class="pc-conditions"><template v-for="(value, key) in conditions?.parameters" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></template><dt>共同协议指纹</dt><dd>{{ conditions?.common_protocol_hash }}</dd><dt>近期历史窗口</dt><dd>{{ conditions?.history_limit }} 条消息</dd></dl></details>
      </section>

      <p v-if="error || run?.error" class="pc-error" role="alert">{{ error || run?.error }}</p>
      <p v-if="connecting" class="pc-progress" role="status">连接暂时中断，正在重新获取本次记录…</p>
      <div v-if="run" class="pc-run-heading"><div><h2>{{ run.scenario.title }}</h2><p>{{ statusNames[run.status] || run.status }} · {{ completed }} / {{ total }} 份回答 · {{ run.repeats }} 次重复</p></div><small v-if="running">离开页面会停止当前对照</small><small v-else>{{ manualDone }} 项人工评阅已记录</small></div>

      <div v-if="run" class="pc-score-grid" aria-label="对照汇总">
        <section v-for="variant in variantIDs" :key="variant" class="pc-score" :class="variant"><h3>{{ variantNames[variant] }}</h3><div class="pc-score-values"><div><strong>{{ fraction(stats[variant].candidatePass, stats[variant].candidateTotal) }}</strong><small>生成时 · 自动检查通过项</small></div><div><strong>{{ fraction(stats[variant].finalPass, stats[variant].finalTotal) }}</strong><small>审校后 · 自动检查通过项</small></div><div><strong>{{ stats[variant].revisions }} 次</strong><small>审校改写 · 含有界纠错</small></div><div><strong>{{ formatMS(stats[variant].firstMS) }}</strong><small>首段文字等待 · 中位数</small></div></div><p>{{ stats[variant].completed }} 轮完成 · {{ stats[variant].errors }} 轮未完成 · {{ stats[variant].fallbacks }} 轮回退 · {{ stats[variant].manual }} 项需人工评阅</p></section>
      </div>

      <section v-for="round in run?.rounds" :key="round.index" class="pc-round" :aria-label="`第 ${round.turn} 轮对照`">
        <header class="pc-round-header"><span>第 {{ round.turn }} 轮<small v-if="run && run.repeats > 1"> · 第 {{ round.repeat }} 次重复</small></span><span class="pc-match" :class="{ good: round.conditions_match, bad: !round.conditions_match && round.legacy.status !== 'running' && round.current.status !== 'running' }">{{ round.conditions_match ? '输入条件已核对一致' : round.legacy.status === 'running' || round.current.status === 'running' ? '正在核对输入' : '条件未匹配 · 不计入汇总' }}</span></header>
        <div class="pc-user"><small>共同用户话语</small><p>{{ round.user }}</p></div>
        <details class="pc-context"><summary>本轮共同历史与风险检查</summary><ol><li v-for="(message, n) in round.history" :key="n"><b>{{ message.role === 'user' ? '用户' : '共同参考答复' }}</b><span>{{ message.content }}</span></li></ol><p>共同风险结果：{{ riskNames[round.risk || ''] || '检查中' }} · {{ formatMS(round.risk_ms || undefined) }} · 每对回答只检查一次</p><p v-if="round.risk_error" class="pc-error">{{ round.risk_error }}</p><p>共同条件指纹：{{ round.legacy.conditions_hash || '…' }} / {{ round.current.conditions_hash || '…' }}</p></details>
        <div class="pc-answer-grid">
          <article v-for="variant in variantIDs" :key="variant" class="pc-answer" :class="variant" :aria-label="`${variantNames[variant]}第 ${round.turn} 轮`">
            <header><h3>{{ variantNames[variant] }}</h3><span class="pc-leg-state" :class="round[variant].status">{{ statusNames[round[variant].status] || round[variant].status }}</span></header>
            <p class="pc-answer-text" :class="{ empty: !round[variant].text }" aria-live="polite">{{ round[variant].text || (round[variant].status === 'running' ? '正在生成并检查…' : '本轮没有完整回答') }}<span v-if="round[variant].status === 'running' && round[variant].text" class="pc-cursor" /></p>
            <p v-if="round[variant].error || round[variant].fallback" class="pc-error">{{ round[variant].error || '本轮使用了回退回应，保留记录。' }}</p>
            <dl class="pc-leg-metrics"><div><dt>首段文字</dt><dd>{{ formatMS(round[variant].first_text_ms) }}</dd></div><div><dt>完整回答</dt><dd>{{ formatMS(round[variant].completion_ms) }}</dd></div><div><dt>审校改写</dt><dd>{{ round[variant].revisions }} 次</dd></div><div><dt>额外纠错</dt><dd>{{ round[variant].repair_requests }} 次</dd></div></dl>
            <details v-if="round[variant].candidate && round[variant].status !== 'running'" class="pc-candidate"><summary>查看审校前的回答</summary><p>{{ round[variant].candidate }}</p></details>
            <table class="pc-check-table"><caption>逐项验收</caption><thead><tr><th>检查项</th><th>生成时</th><th>审校后</th></tr></thead><tbody><tr v-for="check in checks(round, variant)" :key="check.id"><th scope="row">{{ check.label }}<small v-if="check.kind === 'manual'">人工项</small></th><td data-check-stage="candidate"><ComparisonCheckCell :check="candidate(round[variant], check.id)" :review="reviews[reviewKey(round.index, variant, 'candidate', check.id)]" @review="review(reviewKey(round.index, variant, 'candidate', check.id), $event)" /></td><td data-check-stage="final"><ComparisonCheckCell :check="check" :review="reviews[reviewKey(round.index, variant, 'final', check.id)]" @review="review(reviewKey(round.index, variant, 'final', check.id), $event)" /></td></tr></tbody></table>
            <details class="pc-trace"><summary>查看调用与耗时记录 · {{ round[variant].requests }} 次独立请求</summary><ul><li v-for="(phase, n) in round[variant].phases" :key="n"><span>{{ phase.name }}</span><b>{{ formatMS(phase.elapsed_ms) }} · {{ phase.ok ? '完成' : '未通过' }}</b></li></ul><small>共同风险请求单独记在本轮历史中；改写次数按审校返回 revise 计数，额外纠错是其中的请求子集。</small></details>
          </article>
        </div>
      </section>

      <section v-if="!run" class="pc-empty"><div aria-hidden="true">A <span>↔</span> B</div><h2>让差异落在实际回答上</h2><p>选择一组情境开始。两版并行回答，逐轮核对要求、查看被修改的内容和等待时间。</p></section>
      <footer class="pc-footnote">自动检查只覆盖明确可计算的规则；“通过”不代表共情或帮助程度已得到证明。语义项请独立评阅，所有失败与回退均保留。延迟仅为文字等待，不包含语音起播；不对应 75% 干预有效率。</footer>
    </main>
  </div>
</template>
