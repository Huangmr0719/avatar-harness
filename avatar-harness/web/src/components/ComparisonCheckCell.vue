<script setup lang="ts">
import { computed } from 'vue'
import { checkNames, type CheckResult } from '../promptComparison'
const props = defineProps<{ check?: CheckResult; review?: 'pass' | 'fail' }>()
const emit = defineEmits<{ review: [value?: 'pass' | 'fail'] }>()
const status = computed(() => props.check?.status === 'manual' ? props.review || 'manual' : props.check?.status || 'not_run')
</script>
<template>
  <span class="pc-check" :class="status" :title="check?.detail">{{ check?.status === 'manual' && review ? '人工 · ' : '' }}{{ check ? checkNames[status] : '—' }}</span>
  <div v-if="check?.status === 'manual'" class="pc-review-actions" aria-label="人工评阅">
    <button type="button" :aria-pressed="review === 'pass'" @click="emit('review', 'pass')">符合</button>
    <button type="button" :aria-pressed="review === 'fail'" @click="emit('review', 'fail')">不符</button>
    <button v-if="review" type="button" aria-label="撤销评阅" @click="emit('review')">撤销</button>
  </div>
</template>
