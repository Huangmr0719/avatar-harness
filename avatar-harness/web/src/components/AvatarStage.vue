<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import type { AudioSegment, MotionFrame, Renderer } from '../types'
import type { AvatarView } from '../scene'
import type { LamView } from '../lam'
import type { ExpressionStyle } from '../expressionControl'
const props = defineProps<{ renderer: Renderer; turnId: string | null; resetToken: number; expression?: ExpressionStyle; intensity?: number; pad?: Record<string, number> }>()
const emit = defineEmits<{ status: [state: string, message?: string]; driver: [state: string, message?: string] }>()
const host = ref<HTMLElement | null>(null)
let view: AvatarView | LamView | null = null
let generation = 0
watch(() => props.renderer, async (renderer) => {
  const current = ++generation
  view?.dispose(); view = null
  if ((renderer.kind === 'scene' || renderer.kind === 'lam') && renderer.asset_url) {
    emit('status', 'loading')
    try {
      const View = renderer.kind === 'lam' ? (await import('../lam')).LamView : (await import('../scene')).AvatarView
      if (current !== generation || !host.value) return
      view = new View(host.value, (state, message) => emit('status', state, message), (state, message) => emit('driver', state, message), props.pad)
      if ('setExpression' in view) view.setExpression(props.expression ?? 'neutral', props.intensity ?? 1)
      await view.load(renderer.asset_url)
    } catch (error) { if (current === generation) emit('status', 'error', String(error)) }
  } else emit('status', renderer.kind === 'none' ? 'none' : 'loading')
}, { immediate: true, flush: 'post' })
watch(() => props.turnId, () => view?.clearMotion())
watch(() => props.resetToken, () => view?.reset())
watch(() => [props.expression, props.intensity] as const, ([style, intensity]) => { if (view && 'setExpression' in view) view.setExpression(style ?? 'neutral', intensity ?? 1) })
defineExpose({ frames: (frames: MotionFrame[], base: number) => view?.addFrames(frames, base), audio: (segment: AudioSegment) => view?.addAudio(segment), prepare: async (buffer: AudioBuffer, pts: number) => { if (view && 'prepare' in view) return await view.prepare(buffer, pts) }, probe: () => view?.probe() ?? null })
onBeforeUnmount(() => { generation++; view?.dispose() })
</script>
<template>
  <div class="render-surface">
    <div ref="host" class="three-surface" :class="{ visible: renderer.kind === 'scene' || renderer.kind === 'lam' }" aria-label="数字人 3D 渲染画面" />
    <iframe v-if="renderer.kind === 'iframe' && renderer.url" :src="renderer.url" title="数字人渲染服务" allow="autoplay" @load="emit('status', 'embedded')" />
    <video v-if="renderer.kind === 'video' && renderer.url" :src="renderer.url" autoplay playsinline :muted="!renderer.includes_audio" controls aria-label="数字人视频流" @loadeddata="emit('status', 'ready')" @error="emit('status', 'error', '视频加载失败，请检查地址、格式与浏览器访问权限。')" />
  </div>
</template>
