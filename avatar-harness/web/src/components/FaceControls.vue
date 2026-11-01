<script setup lang="ts">
import { ref } from 'vue'
import { FACE_REGIONS, HEAD_AXES, type HeadOverrides } from '../faceRig'
const props = defineProps<{ supported: string[]; live: Record<string, number>; overrides: Record<string, number>; headSupported: boolean; headLive: HeadOverrides; headOverrides: HeadOverrides }>()
const emit = defineEmits<{ change: [name: string, value: number]; head: [name: string, value: number]; action: [name: string]; reset: [region?: string]; close: [] }>()
const opened = ref<string[]>(['brows'])
const ids = ['head', ...FACE_REGIONS.map(region => region.id)]
function toggle(id: string, event: Event) {
  const open = (event.target as HTMLDetailsElement).open
  if (open !== opened.value.includes(id)) opened.value = open ? [...opened.value, id] : opened.value.filter(value => value !== id)
}
const has = (name: string) => props.supported.includes(name)
const manual = (name: string) => Object.hasOwn(props.overrides, name)
const value = (name: string) => props.overrides[name] ?? props.live[name] ?? 0
const percent = (n: number) => `${Math.round(n * 100)}%`
</script>
<template>
  <section id="face-controls-panel" class="face-controls" aria-label="面部部位调节">
    <header><div><strong>部位调节</strong><small>{{ supported.length }} / 52 项面部调节</small></div><button aria-label="关闭部位调节" @click="emit('close')">关闭</button></header>
    <p>拖动后手动控制对应部位；关闭面板恢复自动。嘴部调节会影响口型。</p>
    <div class="brow-actions"><button @click="emit('action', 'raise')">抬眉</button><button @click="emit('action', 'inner')">提内眉</button><button @click="emit('action', 'frown')">皱眉</button><button @click="emit('reset')">恢复自动</button></div>
    <div class="region-toolbar"><span>头部与全部面部部位</span><button @click="opened = opened.length === ids.length ? [] : [...ids]">{{ opened.length === ids.length ? '收起全部' : '展开全部' }}</button></div>
    <div class="face-regions">
      <details :open="opened.includes('head')" @toggle="toggle('head', $event)">
        <summary>头部姿态<small>{{ headSupported ? '3 个方向' : '形象未提供' }}</small></summary>
        <p class="head-control-hint">在原动作上调整角度；恢复自动后继续聆听反馈。</p>
        <div v-for="axis in HEAD_AXES" :key="axis.id" class="face-channel" :class="{ unavailable: !headSupported }">
          <label :for="`head-${axis.id}`">{{ axis.label }}<small>{{ Object.hasOwn(headOverrides, axis.id) ? '手动' : '自动' }}</small></label>
          <div><input :id="`head-${axis.id}`" type="range" :min="axis.min" :max="axis.max" step="0.5" :value="headOverrides[axis.id] ?? 0" :disabled="!headSupported" :aria-label="axis.label" @input="emit('head', axis.id, Number(($event.target as HTMLInputElement).value))"><output title="当前实际角度">{{ Math.round(headLive[axis.id] ?? 0) }}°</output></div>
        </div>
        <button class="region-reset" @click="emit('reset', 'head')">头部恢复自动</button>
      </details>
      <details v-for="region in FACE_REGIONS" :key="region.id" :open="opened.includes(region.id)" @toggle="toggle(region.id, $event)">
        <summary>{{ region.label }}<small>{{ region.channels.filter(([name]) => has(name)).length }} 项</small></summary>
        <div v-for="[name, label] in region.channels" :key="name" class="face-channel" :class="{ unavailable: !has(name) }">
          <label :for="`rig-${name}`" :title="name">{{ label }}<small>{{ has(name) ? manual(name) ? '手动' : '自动' : '资产未提供' }}</small></label>
          <div><input :id="`rig-${name}`" type="range" min="0" max="1" step="0.01" :value="value(name)" :disabled="!has(name)" :aria-label="label" @input="emit('change', name, Number(($event.target as HTMLInputElement).value))"><output :title="`当前驱动值 ${live[name] ?? 0}`">{{ percent(live[name] ?? 0) }}</output></div>
        </div>
        <button class="region-reset" @click="emit('reset', region.id)">此部位恢复自动</button>
      </details>
    </div>
  </section>
</template>
