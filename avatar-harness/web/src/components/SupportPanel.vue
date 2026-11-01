<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { canFeedback, supportDefaults, supportNames, type SupportCommand, type SupportMode, type SupportParameters, type SupportState } from '../intervention'
const props = defineProps<{ state: SupportState; pending: boolean; progress: number; playing: boolean }>()
const emit = defineEmits<{ command: [value: SupportCommand]; video: [value: HTMLVideoElement | null]; play: []; stopVideo: [] }>()
const open = ref(false), mode = ref<SupportMode>('humanistic'), goal = ref(''), parameters = ref<SupportParameters>(supportDefaults('humanistic'))
watch(mode, value => { parameters.value = supportDefaults(value) }, { flush: 'sync' })
watch(() => props.state.version, () => { if (props.state.parameters && props.state.mode) { mode.value = props.state.mode; parameters.value = { ...props.state.parameters }; goal.value = props.state.goal || '' } })
const active = computed(() => !!props.state.mode && props.state.status !== 'stopped')
const feedbackReady = computed(() => canFeedback(props.state) && !props.pending)
function command(operation: string, extra: Partial<SupportCommand> = {}) { emit('command', { operation, ...extra }) }
</script>
<template>
  <section class="support-panel" aria-label="交流支持">
    <button class="support-heading" :aria-expanded="open" @click="open = !open"><span>交流支持</span><small>{{ active ? `${supportNames[state.mode!]} · ${state.status === 'paused' ? '已暂停' : '已启用'}` : '按需开启' }} {{ open ? '−' : '+' }}</small></button>
    <div v-if="open" class="support-body">
      <label>交流方式<select v-model="mode" aria-label="交流方式"><option value="humanistic">倾听支持</option><option value="cbt">认知梳理</option><option value="mindfulness">注意练习 · 视频</option></select></label>
      <label>这次希望获得什么<input v-model="goal" maxlength="160" placeholder="可留空，在对话里说明" aria-label="支持目标"></label>
      <details><summary>调整交流负担</summary>
        <template v-if="mode === 'mindfulness'"><label>练习时长（秒）<input v-model.number="parameters.duration_seconds" type="number" min="15" max="90" step="15" aria-label="练习时长"></label><label>画面节奏<select v-model.number="parameters.playback_rate" aria-label="画面节奏"><option :value="0.75">舒缓 0.75</option><option :value="1">正常 1</option><option :value="1.25">稍快 1.25</option></select></label></template>
        <template v-else><label>每次回应上限（字）<input v-model.number="parameters.max_characters" type="number" min="60" max="400" step="20" aria-label="回应字数上限"></label><label>提问深度<select v-model.number="parameters.question_depth" aria-label="提问深度"><option :value="0">不主动提问</option><option :value="1">轻度澄清</option><option :value="2">共同检查证据</option></select></label><label>每几轮最多提问一次<input v-model.number="parameters.question_every" type="number" min="1" max="4" aria-label="提问间隔"></label><label v-if="mode === 'cbt'">小行动上限（分钟）<input v-model.number="parameters.action_minutes" type="number" min="1" max="5" aria-label="行动分钟上限"></label></template>
      </details>
      <div class="support-actions"><button class="outline" :disabled="pending" @click="command('start', { mode, goal, parameters })">{{ active ? '按此设置重新开始' : '启用支持' }}</button><button v-if="active" class="outline" :disabled="pending" @click="command('stop')">结束支持</button></div>
      <p v-if="state.status === 'paused'" role="status">已暂停练习。可继续交流，或调整后重新开始。</p>
      <div v-if="state.execution?.kind === 'video'" class="practice-video">
        <video :ref="value => emit('video', value as HTMLVideoElement | null)" src="/support/attention-observation.mp4" muted playsinline preload="metadata" aria-label="注意练习视频" />
        <p>自然观察光点或身边物体；走神后轻轻返回。保持自然呼吸，随时可以停止。</p>
        <div class="support-actions"><button v-if="state.execution.status === 'queued'" class="outline" :disabled="pending" @click="emit('play')">开始练习</button><button v-if="playing" class="outline" @click="emit('stopVideo')">停止练习</button><span role="status">{{ Math.floor(progress / 1000) }} / {{ state.execution.parameters.duration_seconds }} 秒 · {{ ({ queued: '待开始', playing: '播放中', completed: '已完成', interrupted: '已停止' } as Record<string, string>)[state.execution.status] || '' }}</span></div>
      </div>
      <div v-if="active && state.execution?.presented" class="support-feedback" aria-label="本次支持反馈"><span>这次体验如何？</span><div><button :disabled="!feedbackReady" @click="command('feedback', { execution_id: state.execution!.id, feedback: 'helpful' })">有帮助</button><button :disabled="!feedbackReady" @click="command('feedback', { execution_id: state.execution!.id, feedback: 'no_help' })">没帮助</button><button :disabled="!feedbackReady" @click="command('feedback', { execution_id: state.execution!.id, feedback: 'too_much' })">负担偏大</button><button :disabled="!feedbackReady" @click="command('feedback', { execution_id: state.execution!.id, feedback: 'worse' })">感觉更差</button></div></div>
      <p v-if="state.suggested_mode">连续反馈没有帮助，可以选择「{{ supportNames[state.suggested_mode] }}」，也可以结束。方式由你选择。</p>
      <div v-if="state.action" class="support-action"><p>{{ state.action.text }} · {{ state.action.minutes }} 分钟内</p><button v-if="state.action.status === 'proposed'" class="outline" :disabled="pending" @click="command('action_agree', { execution_id: state.action!.id })">我愿意尝试</button><div v-else-if="state.action.status === 'agreed'" class="support-actions"><button class="outline" :disabled="pending" @click="command('action_done', { execution_id: state.action!.id })">已尝试</button><button class="outline" :disabled="pending" @click="command('action_not_done', { execution_id: state.action!.id })">未完成</button></div><small v-else>{{ state.action.status === 'reported_done' ? '已记录尝试，帮助程度待回顾' : '已记录未完成，可在对话里讨论障碍' }}</small></div>
      <small v-if="state.last_feedback && active" class="support-reason">{{ ({ helpful: '保持当前方式和负担', no_help: '已记录，会在后续交流中回顾适配性', too_much: '已降低下一次的交流负担', worse: '已暂停，先确认你的需要' } as Record<string, string>)[state.last_feedback] }}</small>
    </div>
  </section>
</template>
