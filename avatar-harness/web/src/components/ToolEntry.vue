<script setup lang="ts">
import type { Entry } from '../types'
import Icon from './Icon.vue'
defineProps<{ entry: Entry }>()
const statuses: Record<string, string> = { running: '执行中', ok: '已完成', error: '失败', timeout: '已超时', cancelled: '已取消' }
</script>
<template>
  <details class="tool-entry">
    <summary><Icon name="tool" :size="16" /><code>{{ entry.name }}</code><span class="tool-state" :class="entry.status">{{ statuses[entry.status ?? 'running'] ?? entry.status }}</span><span v-if="entry.elapsed_ms != null" class="tool-time">{{ entry.elapsed_ms.toFixed(1) }} ms</span><Icon name="down" :size="14" /></summary>
    <div class="tool-body"><label>参数</label><pre>{{ JSON.stringify(entry.args ?? {}, null, 2) }}</pre><template v-if="entry.result"><label>结果</label><pre>{{ JSON.stringify(entry.result, null, 2) }}</pre></template></div>
  </details>
</template>
