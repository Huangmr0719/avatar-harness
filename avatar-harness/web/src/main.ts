import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'

if (location.pathname === '/demo/prompts') {
  document.body.classList.add('prompt-comparison-page')
  void import('./components/PromptComparison.vue').then(({ default: Comparison }) => createApp(Comparison).mount('#app'))
} else createApp(App).mount('#app')
