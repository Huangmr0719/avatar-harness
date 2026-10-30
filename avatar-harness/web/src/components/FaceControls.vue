<script setup lang="ts">
import { FACE_REGIONS } from '../faceRig'
const props = defineProps<{ supported: string[]; live: Record<string, number>; overrides: Record<string, number> }>()
const emit = defineEmits<{ change: [name: string, value: number]; action: [name: string]; reset: [region?: string]; close: [] }>()
const has = (name: string) => props.supported.includes(name)
const manual = (name: string) => Object.hasOwn(props.overrides, name)
const value = (name: string) => props.overrides[name] ?? props.live[name] ?? 0
const percent = (n: number) => `${Math.round(n * 100)}%`
</script>
<template>
  <section class="face-controls" aria-label="面部部位调节">
    <header><div><strong>部位调节</strong><small>{{ supported.length }} / 52 通道可用</small></div><button aria-label="关闭部位调节" @click="emit('close')">关闭</button></header>
    <p>拖动后接管该通道；关闭面板恢复自动。调节嘴部时会影响口型。</p>
    <div class="brow-actions"><button @click="emit('action', 'raise')">抬眉</button><button @click="emit('action', 'inner')">提内眉</button><button @click="emit('action', 'frown')">皱眉</button><button @click="emit('reset')">恢复自动</button></div>
    <p class="rig-limit">额头随眉部联动。此形象没有独立的动态皱纹通道。</p>
    <div class="face-regions">
      <details v-for="region in FACE_REGIONS" :key="region.id" :open="region.id === 'brows'">
        <summary>{{ region.label }}<small>{{ region.channels.filter(([name]) => has(name)).length }} 通道</small></summary>
        <div v-for="[name, label] in region.channels" :key="name" class="face-channel" :class="{ unavailable: !has(name) }">
          <label :for="`rig-${name}`" :title="name">{{ label }}<small>{{ has(name) ? manual(name) ? '手动' : '自动' : '资产未提供' }}</small></label>
          <div><input :id="`rig-${name}`" type="range" min="0" max="1" step="0.01" :value="value(name)" :disabled="!has(name)" :aria-label="label" @input="emit('change', name, Number(($event.target as HTMLInputElement).value))"><output :title="`当前驱动值 ${live[name] ?? 0}`">{{ percent(live[name] ?? 0) }}</output></div>
        </div>
        <button class="region-reset" @click="emit('reset', region.id)">此部位恢复自动</button>
      </details>
    </div>
  </section>
</template>
