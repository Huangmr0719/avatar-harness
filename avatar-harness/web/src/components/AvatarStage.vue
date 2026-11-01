<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import type { AudioSegment, MotionFrame, Renderer } from '../types'
import type { AvatarView } from '../scene'
import type { LamView } from '../lam'
import type { ExpressionStyle } from '../expressionControl'
import type { HeadOverrides, FaceRigSnapshot } from '../faceRig'
const props = defineProps<{ renderer: Renderer; turnId: string | null; resetToken: number; expression?: ExpressionStyle; intensity?: number; pad?: Record<string, number>; listening?: boolean; userSpeaking?: boolean }>()
const emit = defineEmits<{ status: [state: string, message?: string]; driver: [state: string, message?: string] }>()
const host = ref<HTMLElement | null>(null)
let view: AvatarView | LamView | null = null
let generation = 0
let swap: Promise<void> = Promise.resolve()
watch(() => props.renderer, async (renderer) => {
  const current = ++generation
  const cleanup = view?.dispose(); view = null
  if (renderer.kind === 'scene' || renderer.kind === 'lam') emit('status', 'loading')
  // The SDK factory cannot abort an in-flight load. Retire it fully before a
  // replacement uses the same host, and skip selections superseded meanwhile.
  swap = swap.then(async () => {
    try {
      await cleanup
      if (current !== generation || !host.value) return
      if ((renderer.kind === 'scene' || renderer.kind === 'lam') && renderer.asset_url) {
        const status = (state: string, message?: string) => { if (current === generation) emit('status', state, message) }
        const driver = (state: string, message?: string) => { if (current === generation) emit('driver', state, message) }
        if (renderer.kind === 'lam') {
          const { LamView } = await import('../lam')
          if (current !== generation || !host.value) return
          view = new LamView(host.value, status, driver, props.pad, renderer.driver_url, renderer.speech_jaw_max)
        } else {
          const { AvatarView } = await import('../scene')
          if (current !== generation || !host.value) return
          view = new AvatarView(host.value, status)
        }
        if ('setExpression' in view) view.setExpression(props.expression ?? 'neutral', props.intensity ?? 1)
        if ('setAttention' in view) view.setAttention(!!props.listening, !!props.userSpeaking)
        await view.load(renderer.asset_url)
      } else emit('status', renderer.kind === 'none' ? 'none' : 'loading')
    } catch (error) { if (current === generation) emit('status', 'error', String(error)) }
  })
  await swap
}, { immediate: true, flush: 'post' })
watch(() => props.turnId, () => view?.clearMotion())
watch(() => props.resetToken, () => view?.reset())
watch(() => [props.expression, props.intensity] as const, ([style, intensity]) => { if (view && 'setExpression' in view) view.setExpression(style ?? 'neutral', intensity ?? 1) })
watch(() => [props.listening, props.userSpeaking] as const, ([listening, speaking]) => { if (view && 'setAttention' in view) view.setAttention(!!listening, !!speaking) })
async function reconnectDriver() { if (view && 'reconnectDriver' in view) await view.reconnectDriver() }
defineExpose({ reconnectDriver, clearMotion: () => view?.clearMotion(), comparison: (active: boolean) => { if (view && 'setComparison' in view) view.setComparison(active) }, frames: (frames: MotionFrame[], base: number) => view?.addFrames(frames, base), audio: (segment: AudioSegment) => view?.addAudio(segment), prepare: async (buffer: AudioBuffer, pts: number) => { if (view && 'prepare' in view) return await view.prepare(buffer, pts) }, probe: () => view?.probe() ?? null, faceState: (): FaceRigSnapshot | null => view && 'faceState' in view ? view.faceState() : null, overrideFace: (values: Record<string, number>) => { if (view && 'setFaceOverrides' in view) view.setFaceOverrides(values) }, overrideHead: (values: HeadOverrides) => { if (view && 'setHeadOverrides' in view) view.setHeadOverrides(values) } })
onBeforeUnmount(() => { generation++; view?.dispose() })
</script>
<template>
  <div class="render-surface">
    <div ref="host" class="three-surface" :class="{ visible: renderer.kind === 'scene' || renderer.kind === 'lam' }" aria-label="数字人 3D 渲染画面" />
    <iframe v-if="renderer.kind === 'iframe' && renderer.url" :src="renderer.url" title="数字人渲染服务" allow="autoplay" @load="emit('status', 'embedded')" />
    <video v-if="renderer.kind === 'video' && renderer.url" :src="renderer.url" autoplay playsinline :muted="!renderer.includes_audio" controls aria-label="数字人视频流" @loadeddata="emit('status', 'ready')" @error="emit('status', 'error', '视频加载失败，请检查地址、格式与浏览器访问权限。')" />
  </div>
</template>
