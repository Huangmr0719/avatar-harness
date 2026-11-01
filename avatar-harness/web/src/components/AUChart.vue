<script setup lang="ts">
const props = defineProps<{ values?: Record<string, number> | null }>()
const categories = ['AU01','AU02','AU04','AU05','AU06','AU09','AU12','AU15','AU17','AU20','AU25','AU26']
function value(name: string) {
  const n = props.values?.[name]
  return n != null && Number.isFinite(n) ? Math.max(0, Math.min(5, n)) : null
}
</script>
<template>
  <section class="au-chart" aria-label="AU 面部动作强度柱状图">
    <header><strong>面部动作 · AU</strong><span>0–5 <small v-if="!values">· 等待观察</small></span></header>
    <div class="au-chart-plot" role="list" aria-label="固定 12 类 AU">
      <div v-for="name in categories" :key="name" class="au-chart-column" role="listitem" :aria-label="`${name}：${value(name) == null ? '暂无观察' : value(name)?.toFixed(2)}，量程 0 到 5`" :title="`${name} · ${value(name)?.toFixed(2) ?? '暂无观察'}`">
        <span class="au-chart-number">{{ value(name)?.toFixed(1) ?? '—' }}</span>
        <div class="au-chart-track"><i :style="{ height: `${(value(name) ?? 0) * 20}%` }" /></div>
        <span class="au-chart-label">{{ name }}</span>
      </div>
    </div>
  </section>
</template>
<style scoped>
.au-chart{width:100%;padding-top:3px}.au-chart header{display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;font-size:11px}.au-chart header strong{font-weight:500;color:var(--text)}.au-chart header span{font-variant-numeric:tabular-nums}.au-chart header small{font-size:10px}.au-chart-plot{display:grid;grid-template-columns:repeat(12,minmax(0,1fr));gap:4px}.au-chart-column{min-width:0;display:flex;flex-direction:column;align-items:center;gap:4px}.au-chart-number{font:10px/1.2 ui-monospace,monospace;font-variant-numeric:tabular-nums}.au-chart-track{position:relative;width:100%;height:90px;border-bottom:1px solid #52616a;background:repeating-linear-gradient(to top,transparent 0,transparent calc(20% - 1px),#35434b55 calc(20% - 1px),#35434b55 20%)}.au-chart-track i{position:absolute;bottom:0;left:20%;width:60%;background:#7ce3b2;border-radius:2px 2px 0 0;transition:height .45s ease;will-change:height}.au-chart-label{font:8px/1.2 ui-monospace,monospace;white-space:nowrap;color:#a7b5be}@media(prefers-reduced-motion:reduce){.au-chart-track i{transition:none}}
</style>
