<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { expressionCommand, expressionSuggestions } from '../expressionCommands'
import type { ExpressionMode } from '../expressionControl'
const props = defineProps<{ modelValue: string; connected: boolean; expressionsEnabled: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; send: []; expression: [mode: ExpressionMode] }>()
const input = ref<HTMLInputElement | null>(null), selected = ref(0), dismissed = ref(false), composing = ref(false)
const options = computed(() => expressionSuggestions(props.modelValue))
const open = computed(() => !dismissed.value && props.modelValue.trimStart().startsWith('/'))
watch(() => props.modelValue, () => { selected.value = 0; dismissed.value = false })
async function choose(mode: ExpressionMode) {
  if (!props.expressionsEnabled) return
  emit('expression', mode); emit('update:modelValue', ''); dismissed.value = true
  await nextTick(); input.value?.focus()
}
function submit() {
  if (composing.value) return
  const command = expressionCommand(props.modelValue)
  if (command) { void choose(command); return }
  if (props.connected && props.modelValue.trim()) emit('send')
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing || composing.value || event.keyCode === 229 || !open.value) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); dismissed.value = true; return }
  if (!options.value.length) return
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    selected.value = (selected.value + (event.key === 'ArrowDown' ? 1 : options.value.length - 1)) % options.value.length
    void nextTick(() => document.getElementById(`expression-option-${selected.value}`)?.scrollIntoView({ block: 'nearest' }))
  } else if (event.key === 'Enter') { event.preventDefault(); void choose(options.value[selected.value]!.id) }
}
</script>
<template>
  <form class="log-composer" @submit.prevent="submit">
    <div v-if="open" id="expression-options" class="expression-menu" role="listbox" aria-label="选择展示表情">
      <header><strong>展示表情</strong><small>↑ ↓ 选择 · Enter 展示 · Esc 收起</small></header>
      <p v-if="!expressionsEnabled">人物准备好后可选择表情。</p>
      <button v-for="(item, index) in options" :id="`expression-option-${index}`" :key="item.id" type="button" role="option" :aria-selected="selected === index" :disabled="!expressionsEnabled" @mousedown.prevent @click="choose(item.id)">
        <span><strong>{{ item.label }}</strong><small>{{ item.hint }}</small></span><code>/{{ item.id }}</code>
      </button>
      <p v-if="!options.length">没有匹配表情；回车仍可发送其他命令。</p>
    </div>
    <input ref="input" :value="modelValue" role="combobox" aria-label="输入文字" aria-autocomplete="list" aria-haspopup="listbox" :aria-expanded="open" :aria-controls="open ? 'expression-options' : undefined" :aria-activedescendant="open && options.length ? `expression-option-${selected}` : undefined" placeholder="输入文字，或 / 选择表情…" autocomplete="off" @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)" @keydown="keydown" @blur="dismissed = true" @focus="dismissed = false" @compositionstart="composing = true" @compositionend="composing = false">
    <button type="submit" aria-label="发送文字" :disabled="!modelValue.trim() || (!connected && !expressionCommand(modelValue))"><Icon name="send" :size="20" /></button>
  </form>
</template>
