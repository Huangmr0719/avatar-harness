<script setup lang="ts">
import { computed } from 'vue'
import { EMOTION_DEMO, demoRunning, type DemoState } from '../emotionDemo'
const props = defineProps<{ state: DemoState; weights: Record<string, number>; details?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const active = computed(() => demoRunning(props.state))
const step = computed(() => EMOTION_DEMO[props.state.index])
const phase = computed(() => ({ idle: '', preparing: '准备语音', playing: '正在播放', holding: '表情停留', complete: '演示完成', cancelled: '已停止', error: '演示失败' }[props.state.phase]))
const percent = (...names: string[]) => `${Math.round(Math.max(0, ...names.map(name => props.weights[name] ?? 0)) * 100)}%`
</script>
<template>
  <section class="emotion-demo-panel" aria-label="七类情绪自动演示">
    <header><strong>{{ active ? `${state.index + 1} / 7 · ${step?.label}` : phase }}</strong><span>同一句话 · 夸张表达</span><small aria-live="polite">{{ phase }}</small><button v-if="!active" aria-label="关闭情绪演示面板" @click="emit('close')">关闭</button></header>
    <ol aria-label="演示顺序"><li v-for="(item, index) in EMOTION_DEMO" :key="item.style" :class="{ current: active && index === state.index, done: state.phase === 'complete' || index < state.index }">{{ item.label }}</li></ol>
    <p v-if="active" class="demo-line">“{{ step?.text }}”</p>
    <p v-else-if="state.phase === 'error'" class="demo-error">{{ details ? state.message : '演示暂时无法继续，请稍后重试。' }}</p>
    <p v-else>{{ state.phase === 'complete' ? '七种情绪已播完，可再次点击演示重播。' : '已恢复对话模式。' }}</p>
    <div v-if="details && (state.phase === 'playing' || state.phase === 'holding')" class="demo-face-readings" aria-label="演示眉眼驱动值"><span>提眉 <b>{{ percent('browInnerUp', 'browOuterUpLeft', 'browOuterUpRight') }}</b></span><span>压眉 <b>{{ percent('browDownLeft', 'browDownRight') }}</b></span><span>睁眼 <b>{{ percent('eyeWideLeft', 'eyeWideRight') }}</b></span><span>眯眼 <b>{{ percent('eyeSquintLeft', 'eyeSquintRight') }}</b></span><small>额头随眉部联动</small></div>
  </section>
</template>
