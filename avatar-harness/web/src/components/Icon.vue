<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 })
const paths: Record<string, string[]> = {
  plus: ['M12 5v14M5 12h14'],
  message: ['M21 15a4 4 0 0 1-4 4H7l-4 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z'],
  trash: ['M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7'],
  stop: ['M6 6h12v12H6z'],
  link: ['M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2'],
  send: ['m22 2-7 20-4-9-9-4 20-7ZM22 2 11 13'],
  down: ['m6 9 6 6 6-6'],
  right: ['m9 6 6 6-6 6'],
  user: ['M20 21v-2a7 7 0 0 0-14 0v2z', 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z'],
  terminal: ['M3 3h18v18H3zM3 7h18M7 11l3 3-3 3M13 17h4'],
  tool: ['M14 6a5 5 0 0 0-6 6l-5 5a2 2 0 0 0 4 4l5-5a5 5 0 0 0 6-6l-4 2-2-2 2-4z'],
  close: ['m6 6 12 12M6 18 18 6'],
  mic: ['M9 5a3 3 0 0 1 6 0v7a3 3 0 0 1-6 0V5ZM5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8'],
  camera: ['M3 6h12a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H3a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2ZM17 10l6-4v12l-6-4'],
  upload: ['M12 16V3m-5 5 5-5 5 5M3 15v6h18v-6'],
  reset: ['M3 11a9 9 0 1 1 2 7M3 4v7h7'],
  bust: ['M17 6a5 5 0 1 1-10 0 5 5 0 0 1 10 0ZM1 23c0-5 3-8 11-8s11 3 11 8'],
  prompt: ['m4 5 7 7-7 7M13 20h7'],
  'plus-circle': ['M12 2a10 10 0 1 1 0 20 10 10 0 0 1 0-20ZM12 7v10M7 12h10'],
}
</script>
<template><svg :width="size" :height="size" :viewBox="name === 'terminal' ? '0 0 120 90' : '0 0 24 24'" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><template v-if="name === 'terminal'"><rect x="3" y="3" width="114" height="84" rx="4" /><path d="M3 23h114" /><circle cx="14" cy="13" r="2" fill="currentColor" /><circle cx="25" cy="13" r="2" fill="currentColor" /><circle cx="36" cy="13" r="2" fill="currentColor" /><path d="m22 46 10 8-10 8M43 68h15" stroke="#a6d79a" stroke-width="2.5" /></template><path v-for="(path, index) in name === 'terminal' ? [] : paths[name] ?? paths.terminal" :key="index" :d="path" /></svg></template>
